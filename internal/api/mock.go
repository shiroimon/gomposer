package api

import (
	"fmt"
	"time"

	"github.com/shiroimon/gomposer/internal/model"
)

type MockDataSource struct {
	DAGs          []model.DAG
	DAGRuns       map[string][]model.DAGRun       // key: dagID
	TaskInstances map[string][]model.TaskInstance // key: dagID/runID
	TaskLogs      map[string]string               // key: dagID/runID/taskID
}

func NewMockDataSource() *MockDataSource {
	now := time.Now()

	dags := []model.DAG{
		{ID: "etl_patient_records", IsPaused: false, LastRunState: "success", LastRunDate: now.Add(-1 * time.Hour), ScheduleInterval: "0 2 * * *", NextDagRun: now.Add(23 * time.Hour)},
		{ID: "etl_prescription_sync", IsPaused: false, LastRunState: "failed", LastRunDate: now.Add(-2 * time.Hour), ScheduleInterval: "@hourly", NextDagRun: now.Add(58 * time.Minute)},
		{ID: "analytics_daily_report", IsPaused: false, LastRunState: "running", LastRunDate: now.Add(-30 * time.Minute), ScheduleInterval: "@daily", NextDagRun: now.Add(23*time.Hour + 30*time.Minute)},
		{ID: "data_quality_check", IsPaused: true, LastRunState: "success", LastRunDate: now.Add(-24 * time.Hour), ScheduleInterval: "0 6 * * *"},
		{ID: "master_table_update", IsPaused: false, LastRunState: "success", LastRunDate: now.Add(-3 * time.Hour), ScheduleInterval: "0 */4 * * *", NextDagRun: now.Add(1 * time.Hour)},
		{ID: "report_weekly_summary", IsPaused: false, LastRunState: "success", LastRunDate: now.Add(-6 * time.Hour), ScheduleInterval: "0 0 * * 1"},
	}

	dagRuns := map[string][]model.DAGRun{}
	taskInstances := map[string][]model.TaskInstance{}
	taskLogs := map[string]string{}

	for _, dag := range dags {
		runs := make([]model.DAGRun, 3)
		for i := range 3 {
			runID := fmt.Sprintf("run_%s_%d", dag.ID, i+1)
			states := []string{"success", "success", dag.LastRunState}
			startTime := now.Add(-time.Duration(3-i) * 24 * time.Hour)
			endTime := startTime.Add(15 * time.Minute)
			if i == 2 && dag.LastRunState == "running" {
				endTime = time.Time{}
			}

			runs[i] = model.DAGRun{
				DagID:     dag.ID,
				RunID:     runID,
				State:     states[i],
				StartDate: startTime,
				EndDate:   endTime,
			}

			tasks := make([]model.TaskInstance, 3)
			taskNames := []string{"extract", "transform", "load"}
			for j, taskName := range taskNames {
				taskID := fmt.Sprintf("%s_%s", taskName, dag.ID)
				taskState := runs[i].State
				if taskState == "failed" && j == 2 {
					taskState = "failed"
				} else if taskState == "failed" && j < 2 {
					taskState = "success"
				}
				if taskState == "running" && j == 2 {
					taskState = "running"
				} else if taskState == "running" && j < 2 {
					taskState = "success"
				}
				// Simulate false success: DAG is success but last task is upstream_failed
				if dag.ID == "report_weekly_summary" && i == 2 && j == 2 {
					taskState = "upstream_failed"
				}

				tryNumber := 1
				if taskState == "failed" {
					tryNumber = 2
				}
				tasks[j] = model.TaskInstance{
					TaskID:    taskID,
					DagID:     dag.ID,
					RunID:     runID,
					State:     taskState,
					Duration:  time.Duration(j+1) * 3 * time.Minute,
					StartDate: startTime.Add(time.Duration(j) * 5 * time.Minute),
					TryNumber: tryNumber,
				}

				logKey := fmt.Sprintf("%s/%s/%s", dag.ID, runID, taskID)
				taskLogs[logKey] = generateMockLog(startTime, taskID, taskState)
			}

			tiKey := fmt.Sprintf("%s/%s", dag.ID, runID)
			taskInstances[tiKey] = tasks
		}
		dagRuns[dag.ID] = runs
	}

	return &MockDataSource{
		DAGs:          dags,
		DAGRuns:       dagRuns,
		TaskInstances: taskInstances,
		TaskLogs:      taskLogs,
	}
}

func (m *MockDataSource) ListDAGs() ([]model.DAG, error) {
	return m.DAGs, nil
}

func (m *MockDataSource) ListDAGRuns(dagID string) []model.DAGRun {
	return m.DAGRuns[dagID]
}

func (m *MockDataSource) ListTaskInstances(dagID, runID string) []model.TaskInstance {
	key := fmt.Sprintf("%s/%s", dagID, runID)
	return m.TaskInstances[key]
}

func (m *MockDataSource) GetTaskLog(dagID, runID, taskID string, tryNumber int) string {
	key := fmt.Sprintf("%s/%s/%s", dagID, runID, taskID)
	if log, ok := m.TaskLogs[key]; ok {
		return log
	}
	return "(no log available)"
}

func (m *MockDataSource) TriggerDAG(dagID string, conf map[string]interface{}) (model.DAGRun, error) {
	// Find the DAG
	found := false
	for _, dag := range m.DAGs {
		if dag.ID == dagID {
			found = true
			break
		}
	}
	if !found {
		return model.DAGRun{}, fmt.Errorf("DAG %q not found", dagID)
	}

	now := time.Now()
	runID := fmt.Sprintf("manual__%s__%s", dagID, now.Format("2006-01-02T15:04:05"))
	run := model.DAGRun{
		DagID:     dagID,
		RunID:     runID,
		State:     "running",
		StartDate: now,
	}

	m.DAGRuns[dagID] = append(m.DAGRuns[dagID], run)

	// Update DAG last run info
	for i, dag := range m.DAGs {
		if dag.ID == dagID {
			m.DAGs[i].LastRunState = "running"
			m.DAGs[i].LastRunDate = now
			break
		}
	}

	return run, nil
}

func (m *MockDataSource) SetDAGRunState(dagID, runID, state string) error {
	runs, ok := m.DAGRuns[dagID]
	if !ok {
		return fmt.Errorf("DAG %q not found", dagID)
	}
	for i := range runs {
		if runs[i].RunID == runID {
			runs[i].State = state
			if state == "success" || state == "failed" {
				runs[i].EndDate = time.Now()
			}
			m.DAGRuns[dagID] = runs
			return nil
		}
	}
	return fmt.Errorf("DAG Run %q not found", runID)
}

func (m *MockDataSource) ClearTaskInstances(dagID, runID string, taskIDs []string) error {
	key := fmt.Sprintf("%s/%s", dagID, runID)
	tasks, ok := m.TaskInstances[key]
	if !ok {
		return fmt.Errorf("DAG Run %s/%s not found", dagID, runID)
	}

	clearAll := len(taskIDs) == 0
	taskSet := map[string]bool{}
	for _, id := range taskIDs {
		taskSet[id] = true
	}

	for i := range tasks {
		if clearAll || taskSet[tasks[i].TaskID] {
			tasks[i].State = "running"
			tasks[i].TryNumber++
			tasks[i].StartDate = time.Now()
		}
	}
	m.TaskInstances[key] = tasks
	return nil
}

func (m *MockDataSource) TogglePause(dagID string) (bool, error) {
	for i, dag := range m.DAGs {
		if dag.ID == dagID {
			m.DAGs[i].IsPaused = !m.DAGs[i].IsPaused
			return m.DAGs[i].IsPaused, nil
		}
	}
	return false, fmt.Errorf("DAG %q not found", dagID)
}

func (m *MockDataSource) GetXComEntries(dagID, runID, taskID string) []model.XComEntry {
	return []model.XComEntry{
		{Key: "return_value", Value: `{"rows_processed": 987, "status": "ok"}`},
		{Key: "run_id", Value: runID},
	}
}

func (m *MockDataSource) GetDAGDetail(dagID string) (model.DAGDetail, error) {
	for _, dag := range m.DAGs {
		if dag.ID == dagID {
			tasks := []model.TaskDep{
				{TaskID: "extract_" + dagID, DownstreamIDs: []string{"transform_" + dagID}},
				{TaskID: "transform_" + dagID, DownstreamIDs: []string{"load_" + dagID}},
				{TaskID: "load_" + dagID, DownstreamIDs: nil},
			}
			// Add a branching task for report_weekly_summary to show a more complex graph
			if dagID == "report_weekly_summary" {
				tasks = []model.TaskDep{
					{TaskID: "extract_" + dagID, DownstreamIDs: []string{"transform_" + dagID, "validate_" + dagID}},
					{TaskID: "transform_" + dagID, DownstreamIDs: []string{"load_" + dagID}},
					{TaskID: "validate_" + dagID, DownstreamIDs: []string{"load_" + dagID}},
					{TaskID: "load_" + dagID, DownstreamIDs: nil},
				}
			}
			return model.DAGDetail{
				DagID:     dagID,
				FileToken: "mock_token_" + dagID,
				FileLoc:   "/opt/airflow/dags/" + dagID + ".py",
				Tasks:     tasks,
			}, nil
		}
	}
	return model.DAGDetail{}, fmt.Errorf("DAG %q not found", dagID)
}

func (m *MockDataSource) GetDAGSource(fileToken string) (string, error) {
	return `from airflow import DAG
from airflow.operators.python import PythonOperator
from datetime import datetime, timedelta

default_args = {
    'owner': 'data-team',
    'depends_on_past': False,
    'email_on_failure': True,
    'retries': 2,
    'retry_delay': timedelta(minutes=5),
}

with DAG(
    dag_id='example_dag',
    default_args=default_args,
    description='Sample DAG for Gomposer demo',
    schedule_interval='0 2 * * *',
    start_date=datetime(2024, 1, 1),
    catchup=False,
    tags=['etl', 'production'],
) as dag:

    def extract(**kwargs):
        """Extract data from source systems."""
        print("Extracting data...")
        return {"rows": 987}

    def transform(**kwargs):
        """Transform and validate data."""
        ti = kwargs['ti']
        data = ti.xcom_pull(task_ids='extract')
        print(f"Transforming {data['rows']} rows...")

    def load(**kwargs):
        """Load data into destination."""
        print("Loading data to BigQuery...")

    t1 = PythonOperator(task_id='extract', python_callable=extract)
    t2 = PythonOperator(task_id='transform', python_callable=transform)
    t3 = PythonOperator(task_id='load', python_callable=load)

    t1 >> t2 >> t3
`, nil
}

func (m *MockDataSource) ListImportErrors() []model.ImportError {
	return []model.ImportError{
		{
			Filename:   "/opt/airflow/dags/broken_dag.py",
			StackTrace: "Traceback (most recent call last):\n  File \"/opt/airflow/dags/broken_dag.py\", line 3, in <module>\n    from airflow.operators.nonexistent import FakeOperator\nModuleNotFoundError: No module named 'airflow.operators.nonexistent'",
			Timestamp:  time.Now().Add(-30 * time.Minute),
		},
	}
}

func generateMockLog(startTime time.Time, taskID, state string) string {
	ts := func(offset time.Duration) string {
		return startTime.Add(offset).Format("2006-01-02 15:04:05")
	}

	log := fmt.Sprintf(`[%s] ============================================
[%s] Task: %s
[%s] ============================================
[%s] INFO  - Initializing task environment...
[%s] INFO  - Loading configuration from airflow.cfg
[%s] INFO  - Connecting to database...
[%s] INFO  - Connection established successfully
[%s] INFO  - Checking upstream dependencies...
[%s] INFO  - All upstream tasks completed
[%s] INFO  - Starting main execution...
[%s] INFO  - Processing batch 1/5 (200 records)
[%s] INFO  - Processing batch 2/5 (200 records)
[%s] INFO  - Processing batch 3/5 (200 records)
[%s] INFO  - Processing batch 4/5 (200 records)
[%s] INFO  - Processing batch 5/5 (187 records)
[%s] INFO  - Total records processed: 987
[%s] INFO  - Validating output data...
[%s] INFO  - Data validation passed: 987/987 records OK`,
		ts(0), ts(0), taskID, ts(0),
		ts(1*time.Second),
		ts(2*time.Second),
		ts(3*time.Second),
		ts(4*time.Second),
		ts(5*time.Second),
		ts(6*time.Second),
		ts(10*time.Second),
		ts(30*time.Second),
		ts(50*time.Second),
		ts(70*time.Second),
		ts(90*time.Second),
		ts(110*time.Second),
		ts(120*time.Second),
		ts(125*time.Second),
		ts(130*time.Second),
	)

	switch state {
	case "failed":
		log += fmt.Sprintf(`
[%s] ERROR - Exception occurred during post-processing
[%s] ERROR - Traceback (most recent call last):
[%s] ERROR -   File "/opt/airflow/dags/%s.py", line 42
[%s] ERROR -     result = transform(data)
[%s] ERROR - ValueError: Invalid data format in column 'date_field'
[%s] ERROR - Task failed with exit code 1`,
			ts(135*time.Second),
			ts(135*time.Second),
			ts(135*time.Second), taskID,
			ts(135*time.Second),
			ts(135*time.Second),
			ts(135*time.Second),
		)
	case "running":
		log += fmt.Sprintf(`
[%s] INFO  - Post-processing in progress...
[%s] INFO  - Waiting for external service response...`,
			ts(135*time.Second),
			ts(140*time.Second),
		)
	default:
		log += fmt.Sprintf(`
[%s] INFO  - Post-processing completed
[%s] INFO  - Uploading results to GCS bucket...
[%s] INFO  - Upload complete: gs://composer-data/output/%s/
[%s] INFO  - Cleaning up temporary files...
[%s] INFO  - Task completed successfully`,
			ts(135*time.Second),
			ts(140*time.Second),
			ts(150*time.Second), taskID,
			ts(155*time.Second),
			ts(160*time.Second),
		)
	}

	return log
}
