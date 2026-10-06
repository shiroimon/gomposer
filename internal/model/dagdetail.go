package model

// TaskDep represents a task and its downstream dependencies within a DAG.
type TaskDep struct {
	TaskID        string
	DownstreamIDs []string
}

// DAGDetail holds the task dependency graph and source info for a DAG.
type DAGDetail struct {
	DagID     string
	FileToken string // used to fetch source code via /api/v1/dagSources/{file_token}
	FileLoc   string // file path on the Airflow worker
	Tasks     []TaskDep
}
