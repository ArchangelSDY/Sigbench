package master

import (
	"log"
	"sort"
	"strconv"
	"sync"
	"time"

	"microsoft.com/sigbench/agent"
	"microsoft.com/sigbench/base"
	"microsoft.com/sigbench/sessions"
	"microsoft.com/sigbench/snapshot"
)

type MasterController struct {
	Agents         []*agent.AgentDelegate
	SnapshotWriter snapshot.SnapshotWriter
}

func (c *MasterController) RegisterAgent(address string) error {
	if agentDelegate, err := agent.NewAgentDelegate(address); err == nil {
		c.Agents = append(c.Agents, agentDelegate)
		return nil
	} else {
		return err
	}
}

func (c *MasterController) setupAllAgents(job *base.Job) error {
	var wg sync.WaitGroup

	for _, ag := range c.Agents {
		wg.Add(1)
		go func(ag *agent.AgentDelegate) {
			args := &agent.AgentSetupArgs{
				SessionParams: job.SessionParams,
			}
			var result agent.AgentSetupResult
			if err := ag.Client.Call("AgentController.Setup", args, &result); err != nil {
				// TODO: Report error
				log.Fatalln(err)
			}
			wg.Done()
		}(ag)
	}

	wg.Wait()
	return nil
}

func (c *MasterController) collectCounters(job *base.Job) map[string]int64 {
	counters := make(map[string]int64)
	for _, ag := range c.Agents {
		args := &agent.AgentListCountersArgs{
			SessionNames: job.SessionNames,
		}
		var result agent.AgentListCountersResult
		if err := ag.Client.Call("AgentController.ListCounters", args, &result); err != nil {
			log.Println("ERROR: Fail to list counters from agent:", ag.Address, err)
		}
		for k, v := range result.Counters {
			counters[k] = counters[k] + v
		}
	}

	for _, sessName := range job.SessionNames {
		sess := sessions.SessionMap[sessName]
		if agg, ok := sess.(sessions.CounterAggregator); ok {
			agg.AggregateCounters(job, counters)
		}
	}

	return counters
}

func (c *MasterController) watchCounters(job *base.Job, stopChan chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			counters := c.collectCounters(job)

			if err := c.SnapshotWriter.WriteCounters(time.Now(), counters); err != nil {
				log.Println("Error: fail to write counter snapshot: ", err)
			}

			c.printCounters(counters)
		case <-stopChan:
			return
		}
	}
}

func (c *MasterController) printCounters(counters map[string]int64) {
	table := make([][2]string, 0, len(counters))
	for k, v := range counters {
		table = append(table, [2]string{k, strconv.FormatInt(v, 10)})
	}

	sort.Slice(table, func(i, j int) bool {
		return table[i][0] < table[j][0]
	})

	log.Println("Counters:")
	for _, row := range table {
		log.Println("    ", row[0], ": ", row[1])
	}
}

func (c *MasterController) Run(job *base.Job) error {
	// TODO: Validate job
	var wg sync.WaitGroup
	var agentCount int = len(c.Agents)

	job.StartTime = time.Now()

	if err := c.setupAllAgents(job); err != nil {
		return err
	}

	for idx, ag := range c.Agents {
		wg.Add(1)
		go func(idx int, ag *agent.AgentDelegate) {
			args := &agent.AgentRunArgs{
				Job:        *job,
				AgentCount: agentCount,
				AgentIdx:   idx,
			}
			var result agent.AgentRunResult
			if err := ag.Client.Call("AgentController.Run", args, &result); err != nil {
				// TODO: report error
				log.Println(err)
			}

			wg.Done()
		}(idx, ag)
	}

	stopWatchCounterChan := make(chan struct{})
	go c.watchCounters(job, stopWatchCounterChan)

	wg.Wait()

	close(stopWatchCounterChan)

	log.Println("--- Finished ---")
	counters := c.collectCounters(job)
	c.SnapshotWriter.WriteCounters(time.Now(), counters)
	c.printCounters(counters)

	totalDuration := int64(time.Now().Sub(job.StartTime) / time.Second)
	log.Println("Test duration:", totalDuration, "secs")

	return nil
}
