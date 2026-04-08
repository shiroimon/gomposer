package model

import "time"

type DAGRun struct {
	DagID     string
	RunID     string
	State     string // success, failed, running
	StartDate time.Time
	EndDate   time.Time // zero value if still running
}
