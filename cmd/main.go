package main

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	_ "go.uber.org/automaxprocs"

	"microsoft.com/sigbench/agent"
	"microsoft.com/sigbench/base"
	"microsoft.com/sigbench/master"
	"microsoft.com/sigbench/service"
	"microsoft.com/sigbench/snapshot"
)

func startAsMaster(agents []string, config string, outDir string) {
	if len(agents) == 0 {
		log.Fatalln("No agents specified")
	}
	log.Println("Agents: ", agents)

	// Create output directory
	if err := os.MkdirAll(outDir, 0755); err != nil {
		log.Fatalln(err)
	}
	log.Println("Ouptut directory: ", outDir)

	c := &master.MasterController{
		SnapshotWriter: snapshot.NewJsonSnapshotWriter(outDir + "/counters.txt"),
	}

	for _, agent := range agents {
		if err := c.RegisterAgent(agent); err != nil {
			log.Fatalln("Fail to register agent: ", agent, err)
		}
	}

	var job base.Job
	if f, err := os.Open(config); err == nil {
		decoder := json.NewDecoder(f)
		if err := decoder.Decode(&job); err != nil {
			log.Fatalln("Fail to load config file: ", err)
		}

		// Make a copy of config file to output directory
		copy, err := json.MarshalIndent(job, "", "    ")
		if err != nil {
			log.Fatalln("Fail to encode a copy of config file: ", err)
		}
		if err := ioutil.WriteFile(outDir+"/config.json", copy, 0644); err != nil {
			log.Fatalln("Fail to save a copy of config file: ", err)
		}
	} else {
		log.Fatalln("Fail to open config file: ", err)
	}

	c.Run(&job)

	// j := &sigbench.Job{
	// 	Phases: []sigbench.JobPhase{
	// 		sigbench.JobPhase{
	// 			Name: "First",
	// 			UsersPerSecond: 1,
	// 			Duration: 3 * time.Second,
	// 		},
	// 		sigbench.JobPhase{
	// 			Name: "Second",
	// 			UsersPerSecond: 3,
	// 			Duration: 3 * time.Second,
	// 		},
	// 	},
	// 	SessionNames: []string{
	// 		"dummy",
	// 	},
	// 	SessionPercentages: []float64{
	// 		1,
	// 	},
	// }

	// HTTP
	// j := &sigbench.Job{
	// 	Phases: []sigbench.JobPhase{
	// 		sigbench.JobPhase{
	// 			Name: "Get",
	// 			UsersPerSecond: 10,
	// 			Duration: 10 * time.Second,
	// 		},
	// 	},
	// 	SessionNames: []string{
	// 		"http-get",
	// 	},
	// 	SessionPercentages: []float64{
	// 		1,
	// 	},
	// }

	// j := &sigbench.Job{
	// 	Phases: []sigbench.JobPhase{
	// 		sigbench.JobPhase{
	// 			Name: "Echo",
	// 			UsersPerSecond: 1000,
	// 			Duration: 30 * time.Second,
	// 		},
	// 	},
	// 	SessionNames: []string{
	// 		"signalrcore:echo",
	// 	},
	// 	SessionPercentages: []float64{
	// 		1,
	// 	},
	// }
}

func autoRegisterAgent(agentAddr, masterAddr string) {
	t := time.Tick(5 * time.Second)
	for _ = range t {
		body := url.Values{
			"agentAddress": {agentAddr},
		}
		resp, err := http.PostForm(masterAddr+"/agent/register", body)
		if err != nil {
			log.Println("Fail to register", err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			log.Println("Fail to register: bad status", resp.StatusCode)
			continue
		}
		log.Printf("Register %s to master %s", agentAddr, masterAddr)
	}
}

func startAsAgent(address, masterAddr string) {
	controller := &agent.AgentController{}

	if masterAddr != "" {
		go autoRegisterAgent(address, masterAddr)
	}

	rpc.Register(controller)
	rpc.HandleHTTP()
	l, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatal("Fail to listen:", err)
	}
	http.Serve(l, nil)
}

func startAsService(address string, config *service.ServiceConfig) {
	mux := service.NewServiceMux(config)
	go func() {
		t := time.Tick(5 * time.Second)
		for _ = range t {
			mux.CheckExpiredAgents()
		}
	}()
	log.Fatal(http.ListenAndServe(address, mux))
}

func main() {
	var mode = flag.String("mode", "agent", "service | cli | agent")
	var config = flag.String("config", "config.json", "Job config file")
	var outDir = flag.String("outDir", "output/"+strconv.FormatInt(time.Now().Unix(), 10), "Output directory")
	var listenAddress = flag.String("l", ":7000", "Listen address")
	var agents = flag.String("agents", "", "Agent addresses separated by comma")
	var masterAddr = flag.String("master", "", "Master address for auto registration")
	var influxServerURL = flag.String("influx-server", "", "InfluxDB server URL")
	var influxAuthToken = flag.String("influx-auth-token", "", "InfluxDB auth token")
	var influxOrg = flag.String("influx-org", "", "InfluxDB organization")
	var influxBucket = flag.String("influx-bucket", "", "InfluxDB bucket")

	flag.Parse()

	if mode == nil {
		log.Fatalln("No mode specified")
	}

	if *mode == "cli" {
		log.Println("Start as CLI master")
		startAsMaster(strings.Split(*agents, ","), *config, *outDir)
	} else if *mode == "service" {
		log.Println("Start as service")
		config := &service.ServiceConfig{
			OutDir:          *outDir,
			InfluxServerURL: *influxServerURL,
			InfluxAuthToken: *influxAuthToken,
			InfluxOrg:       *influxOrg,
			InfluxBucket:    *influxBucket,
		}
		startAsService(*listenAddress, config)
	} else {
		log.Println("Start as agent: ", *listenAddress)
		startAsAgent(*listenAddress, *masterAddr)
	}
}
