package base

import "time"

type JobPhase struct {
	Name           string
	UsersPerSecond int64
	Duration       time.Duration
}

type Job struct {
	StartTime          time.Time
	Phases             []JobPhase
	SessionNames       []string
	SessionPercentages []float64
	SessionParams      map[string]string
}

func (j *Job) Duration() time.Duration {
	return time.Now().Sub(j.StartTime)
}
