package agent

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/process"
	"github.com/teris-io/shortid"

	"microsoft.com/sigbench/base"
	"microsoft.com/sigbench/sessions"
)

type AgentController struct {
}

type AgentRunArgs struct {
	Job        base.Job
	AgentCount int
	AgentIdx   int
}

type AgentRunResult struct {
	Error error
}

func (c *AgentController) getSessionUsers(phase *base.JobPhase, percentage float64, agentCount, agentIdx int) int64 {
	totalSessionUsers := int64(float64(phase.UsersPerSecond) * percentage)

	// Ceiling divide
	major := (totalSessionUsers + int64(agentCount) - 1) / int64(agentCount)

	lastIdx := agentCount - 1
	for totalSessionUsers-major*int64(lastIdx) < 0 {
		lastIdx--
	}
	last := totalSessionUsers - int64(lastIdx)*major

	if agentIdx < lastIdx {
		return major
	} else if agentIdx == lastIdx {
		return last
	} else {
		return 0
	}
}

func executeSession(job *base.Job, phase *base.JobPhase, session sessions.Session, done chan struct{}, wg *sync.WaitGroup) {
	uid, err := shortid.Generate()
	if err != nil {
		log.Println("Error: fail to generate uid due to", err)
		return
	}

	ctx := &sessions.UserContext{
		UserId: uid,
		Phase:  phase.Name,
		Params: job.SessionParams,
	}

	// TODO: Check error
	session.Execute(ctx)

	<-done
	wg.Done()
}

func (c *AgentController) runPhase(job *base.Job, phase *base.JobPhase, agentCount, agentIdx int) {
	var swg sync.WaitGroup
	swg.Add(len(job.SessionNames))
	for idx, sessionName := range job.SessionNames {
		sessionUsers := c.getSessionUsers(phase, job.SessionPercentages[idx], agentCount, agentIdx)
		log.Println(fmt.Sprintf("Session %s users: %d", sessionName, sessionUsers))

		var session sessions.Session
		if s, ok := sessions.SessionMap[sessionName]; ok {
			session = s
		} else {
			log.Fatalln("Session not found: " + sessionName)
		}

		after := time.After(phase.Duration)
		sessUserChan := make(chan struct{}, sessionUsers)
		go func(session sessions.Session, swg *sync.WaitGroup) {
			var wg sync.WaitGroup
			defer func() {
				wg.Wait()
				swg.Done()
			}()
			for {
				select {
				case sessUserChan <- struct{}{}:
					wg.Add(1)
					go executeSession(job, phase, session, sessUserChan, &wg)
				case <-after:
					return
				}
			}
		}(session, &swg)
	}
	swg.Wait()
}

func (c *AgentController) Run(args *AgentRunArgs, result *AgentRunResult) error {
	log.Println("Start run: ", args)

	for _, phase := range args.Job.Phases {
		log.Println("Phase: ", phase)
		c.runPhase(&args.Job, &phase, args.AgentCount, args.AgentIdx)
	}

	log.Println("Finished run: ", args)

	return nil
}

type AgentSetupArgs struct {
	SessionParams map[string]string
}

type AgentSetupResult struct {
}

func (c *AgentController) Setup(args *AgentSetupArgs, result *AgentSetupResult) error {
	for _, session := range sessions.SessionMap {
		if err := session.Setup(args.SessionParams); err != nil {
			return err
		}
	}

	return nil
}

type AgentListCountersArgs struct {
	SessionNames []string
}

type AgentListCountersResult struct {
	Counters map[string]int64
}

func (c *AgentController) ListCounters(args *AgentListCountersArgs, result *AgentListCountersResult) error {
	result.Counters = make(map[string]int64)

	// Session counters
	for _, sessionName := range args.SessionNames {
		if session, ok := sessions.SessionMap[sessionName]; ok {
			counters := session.Counters()
			for k, v := range counters {
				result.Counters[k] = result.Counters[k] + v
			}
		}
	}

	// System counters
	sysCounters := c.getSystemCounters()
	for k, v := range sysCounters {
		result.Counters[k] = v
	}

	return nil
}

func (c *AgentController) getSystemCounters() map[string]int64 {
	counters := make(map[string]int64)
	proc, _ := process.NewProcess(int32(os.Getpid()))

	if cpuPercent, err := proc.CPUPercent(); err == nil {
		counters["agent:cpuPercent"] = int64(cpuPercent)
	}

	return counters
}
