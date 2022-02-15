package base

import "time"

type JobPhase struct {
	Name           string
	UsersPerSecond int64
	Duration       string
}

func (p *JobPhase) Validate() error {
	if _, err := time.ParseDuration(p.Duration); err != nil {
		return err
	}
	return nil
}

func (p *JobPhase) GetDuration() time.Duration {
	d, _ := time.ParseDuration(p.Duration)
	return d
}

type Job struct {
	Name               string
	StartTime          time.Time
	Phases             []JobPhase
	SessionNames       []string
	SessionPercentages []float64
	SessionParams      map[string]string
}

func (j *Job) Validate() error {
	for _, p := range j.Phases {
		if err := p.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (j *Job) Duration() time.Duration {
	return time.Now().Sub(j.StartTime)
}
