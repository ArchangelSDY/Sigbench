package base

import "time"

type JobPhase struct {
	Name           string
	UsersPerSecond int64
	Duration       time.Duration
}

type Job struct {
	Phases             []JobPhase
	SessionNames       []string
	SessionPercentages []float64
	SessionParams      map[string]string
}
