package api

import "gomposer/internal/model"

type DataSource interface {
	ListDAGs() ([]model.DAG, error)
	ListDAGRuns(dagID string) []model.DAGRun
	ListTaskInstances(dagID, runID string) []model.TaskInstance
	GetTaskLog(dagID, runID, taskID string, tryNumber int) string
	TriggerDAG(dagID string, conf map[string]interface{}) (model.DAGRun, error)
	TogglePause(dagID string) (bool, error) // returns new IsPaused state
	ClearTaskInstances(dagID, runID string, taskIDs []string) error
	SetDAGRunState(dagID, runID, state string) error
	GetXComEntries(dagID, runID, taskID string) []model.XComEntry
	GetDAGDetail(dagID string) (model.DAGDetail, error)
	GetDAGSource(fileToken string) (string, error)
	ListImportErrors() []model.ImportError
}
