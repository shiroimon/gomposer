package model

import "time"

type DAGRun struct {
	DagID       string
	RunID       string
	State       string // success, failed, running
	StartDate   time.Time
	EndDate     time.Time // zero value if still running
	LogicalDate time.Time
	RunType     string // manual, scheduled, backfill, dataset_triggered
	Conf        string // JSON as returned by the API; "" when the run had no conf
	Note        string
}
