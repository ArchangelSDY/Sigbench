package service

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"microsoft.com/sigbench/base"
	"microsoft.com/sigbench/master"
	"microsoft.com/sigbench/snapshot"
)

func NewServiceMux(outDir string) *SigbenchMux {
	// Create output directory
	if err := os.MkdirAll(outDir, 0755); err != nil {
		log.Fatalln(err)
	}

	sigMux := &SigbenchMux{
		outDir:         outDir,
		snapshotWriter: snapshot.NewMemorySnapshotWriter(),
		wsUpgrader:     &websocket.Upgrader{},
		regAgentAddrs:  make(map[string]time.Time),
		lock:           &sync.RWMutex{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/job/create", sigMux.HandleJobCreate)
	mux.HandleFunc("/job/status", sigMux.HandleJobStatus)
	mux.HandleFunc("/agent/register", sigMux.HandleAgentRegister)
	mux.HandleFunc("/", sigMux.HandleIndex)

	sigMux.mux = mux

	return sigMux
}

type SigbenchMux struct {
	outDir           string
	mux              *http.ServeMux
	masterController *master.MasterController
	snapshotWriter   snapshot.SnapshotWriter
	wsUpgrader       *websocket.Upgrader
	regAgentAddrs    map[string]time.Time
	lock             *sync.RWMutex
}

func (c *SigbenchMux) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	c.mux.ServeHTTP(w, req)
}

func (c *SigbenchMux) renderTemplate(w http.ResponseWriter, tplContent string, data interface{}) error {
	tpl, err := template.New("").Parse(tplContent)
	if err != nil {
		return err
	}

	return tpl.Execute(w, data)
}

const TplIndex = `
<html>
<body>
	<h1>Sigbench</h1>
	<div style="display: flex">
		<form id="job-form" onsubmit="return jobCreate();" style="max-width: 500px">
			<p>Agents</p>
			<textarea name="agents" cols="50" rows="5">{{.agents}}</textarea>

			<p>Config</p>
			<textarea name="config" cols="50" rows="25"></textarea>

			<p>
				<input type="submit" value="Submit">
			</p>
		</form>
		<div style="width: 100%; margin-left: 1em">
			<p>Status</p>
			<textarea id="status" rows="25" style="width: 100%"></textarea>
		</div>
	</div>
	<script>

		function jobCreate() {
			var form = document.getElementById("job-form");
			var agents = form.agents.value;
			var config = form.config.value;

			fetch("/job/create", {
				method: "POST",
				body: new FormData(form)
			}).then(resp => {
				if (!resp.ok) {
					resp.text().then(alert);
				}

				var loc = window.location;
				var statusEl = document.getElementById("status");
				statusEl.value = '';
				window.ws = new WebSocket('ws://' + loc.host + "/job/status");
				window.ws.onmessage = function(ev) {
					var data = JSON.parse(ev.data);
					var counters = data.Counters;
					var updatedAt = data.UpdatedAt;

					var s = updatedAt + ' Counters:\n';
					for (const k in counters) {
						s += (k + ': ' + counters[k] + '\n');
					}
					s += '----------\n';

					statusEl.value += s;
					statusEl.scrollTop = statusEl.scrollHeight;
				};
				window.ws.onclose = function() {
					statusEl.value += 'Finished';
				};
			});

			return false;
		}
	</script>
</body>
</html>
`

func (c *SigbenchMux) HandleIndex(w http.ResponseWriter, req *http.Request) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	agents := []string{}
	for addr := range c.regAgentAddrs {
		agents = append(agents, addr)
	}

	c.renderTemplate(w, TplIndex, map[string]interface{}{
		"agents": strings.Join(agents, ","),
	})
}

func (c *SigbenchMux) HandleJobCreate(w http.ResponseWriter, req *http.Request) {
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	agents := strings.Split(req.Form.Get("agents"), ",")
	config := req.Form.Get("config")

	if len(agents) == 0 {
		http.Error(w, "No agents specified", http.StatusBadRequest)
		return
	}
	log.Println("Agents: ", agents)

	c.lock.RLock()
	if c.masterController != nil {
		c.lock.RUnlock()
		http.Error(w, "A job is still running", http.StatusBadRequest)
		return
	}
	c.lock.RUnlock()

	c.lock.Lock()
	if c.masterController != nil {
		c.lock.Unlock()
		http.Error(w, "A job is still running", http.StatusBadRequest)
		return
	}

	c.snapshotWriter = snapshot.NewMemorySnapshotWriter()
	c.masterController = &master.MasterController{
		// SnapshotWriter: snapshot.NewJsonSnapshotWriter(c.outDir + "/counters.txt"),
		SnapshotWriter: c.snapshotWriter,
	}

	c.lock.Unlock()

	for _, agent := range agents {
		if err := c.masterController.RegisterAgent(agent); err != nil {
			http.Error(w, "Fail to register agent: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	var job base.Job
	if err := json.Unmarshal([]byte(config), &job); err != nil {
		http.Error(w, "Fail to decode config: "+err.Error(), http.StatusBadRequest)
		return
	}

	// // Make a copy of config to output directory
	// if err := ioutil.WriteFile(c.outDir+"/config.json", []byte(config), 0644); err != nil {
	// 	http.Error(w, "Fail to save copy of config: "+err.Error(), http.StatusBadRequest)
	// 	return
	// }

	go func() {
		c.masterController.Run(&job)

		c.lock.Lock()
		c.masterController = nil
		c.lock.Unlock()
	}()

	w.WriteHeader(http.StatusCreated)
}

func (c *SigbenchMux) HandleJobStatus(w http.ResponseWriter, req *http.Request) {
	wc, err := c.wsUpgrader.Upgrade(w, req, nil)
	if err != nil {
		http.Error(w, "Fail to upgrade websocket: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer wc.Close()
	tick := time.Tick(time.Second)
	for _ = range tick {
		lastFlush := false

		var sw *snapshot.MemorySnapshotWriter
		c.lock.RLock()
		lastFlush = c.masterController == nil
		sw = c.snapshotWriter.(*snapshot.MemorySnapshotWriter)
		c.lock.RUnlock()

		payload, err := sw.Dump()
		if err != nil {
			break
		}

		err = wc.WriteMessage(websocket.TextMessage, payload)
		if err != nil {
			break
		}

		if lastFlush {
			break
		}
	}
}

func (c *SigbenchMux) HandleAgentRegister(w http.ResponseWriter, req *http.Request) {
	if err := req.ParseForm(); err != nil {
		http.Error(w, "Fail to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	agentAddr := req.Form.Get("agentAddress")
	if agentAddr != "" {
		c.lock.Lock()
		log.Println("Register agent:", agentAddr)
		c.regAgentAddrs[agentAddr] = time.Now()
		c.lock.Unlock()
	}

	w.WriteHeader(http.StatusOK)
}

func (c *SigbenchMux) CheckExpiredAgents() {
	c.lock.Lock()
	now := time.Now()
	for addr, ts := range c.regAgentAddrs {
		if now.Sub(ts) > 5*time.Minute {
			delete(c.regAgentAddrs, addr)
		}
	}
	c.lock.Unlock()
}
