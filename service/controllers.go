package service

import (
	"encoding/json"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"microsoft.com/sigbench/base"
	"microsoft.com/sigbench/master"
	"microsoft.com/sigbench/snapshot"
)

func NewServiceMux(outDir string) http.Handler {
	// Create output directory
	if err := os.MkdirAll(outDir, 0755); err != nil {
		log.Fatalln(err)
	}

	sigMux := &SigbenchMux{
		outDir: outDir,
		lock:   &sync.RWMutex{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/job/create", sigMux.HandleJobCreate)
	mux.HandleFunc("/", sigMux.HandleIndex)

	sigMux.mux = mux

	return sigMux
}

type SigbenchMux struct {
	outDir           string
	mux              *http.ServeMux
	masterController *master.MasterController
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
	<form id="job-form" onsubmit="return jobCreate();">
		<p>Agents</p>
		<textarea name="agents" cols="50" rows="5">localhost:7000,localhost:7001</textarea>

		<p>Config</p>
		<textarea name="config" cols="50" rows="25"></textarea>

		<p>
			<input type="submit" value="Submit">
		</p>
	</form>
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
			});

			return false;
		}
	</script>
</body>
</html>
`

func (c *SigbenchMux) HandleIndex(w http.ResponseWriter, req *http.Request) {
	c.renderTemplate(w, TplIndex, nil)
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

	c.masterController = &master.MasterController{
		SnapshotWriter: snapshot.NewJsonSnapshotWriter(c.outDir + "/counters.txt"),
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

	// Make a copy of config to output directory
	if err := ioutil.WriteFile(c.outDir+"/config.json", []byte(config), 0644); err != nil {
		http.Error(w, "Fail to save copy of config: "+err.Error(), http.StatusBadRequest)
		return
	}

	go func() {
		c.masterController.Run(&job)

		c.lock.Lock()
		c.masterController = nil
		c.lock.Unlock()
	}()

	w.WriteHeader(http.StatusCreated)
}
