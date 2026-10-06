package model

import "time"

type DAG struct {
	ID               string
	IsPaused         bool
	LastRunState     string    // success, failed, running, or ""
	LastRunDate      time.Time // zero value if never run
	ScheduleInterval string    // cron expression or preset (@daily, etc.)
	NextDagRun       time.Time // zero value if not scheduled
}
