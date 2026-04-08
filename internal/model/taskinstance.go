package model

import "time"

type TaskInstance struct {
	TaskID    string
	DagID     string
	RunID     string
	State     string // success, failed, running, skipped, upstream_failed
	Duration  time.Duration
	StartDate time.Time
	TryNumber int
}
