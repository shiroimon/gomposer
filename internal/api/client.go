package api

import (
	"time"

	"github.com/shiroimon/gomposer/internal/model"
)

type DataSource interface {
	ListDAGs() ([]model.DAG, error)
	ListDAGRuns(dagID string) (runs []model.DAGRun, total int) // newest first, at most dagRunLimit
	ListTaskInstances(dagID, runID string) []model.TaskInstance
	GetTaskLog(dagID, runID, taskID string, tryNumber int) string
	TriggerDAG(dagID string, conf map[string]interface{}) (model.DAGRun, error)
	TogglePause(dagID string) (bool, error) // returns new IsPaused state
	// ClearTaskInstances returns the task IDs it cleared, or would clear when opts.DryRun is set.
	ClearTaskInstances(dagID, runID string, taskIDs []string, opts model.ClearOptions) ([]string, error)
	SetDAGRunState(dagID, runID, state string) error
	SetTaskInstanceState(dagID, runID, taskID, state string) error
	// ListProblemTaskInstances returns task instances across all DAGs in model.ProblemTaskStates started since the given time.
	ListProblemTaskInstances(since time.Time) ([]model.TaskInstance, error)
	GetXComEntries(dagID, runID, taskID string) []model.XComEntry
	GetDAGDetail(dagID string) (model.DAGDetail, error)
	GetDAGSource(fileToken string) (string, error)
	ListImportErrors() []model.ImportError
}
