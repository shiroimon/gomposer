package ui

import (
	"net/url"
	"os/exec"
	"strings"
)

// AirflowGridURL points at the Airflow web UI's grid view. Graphs, Gantt charts and code
// are left to that UI rather than redrawn here. Empty runID/taskID/tab are omitted.
func AirflowGridURL(webserverURL, dagID, runID, taskID, tab string) string {
	q := url.Values{}
	if runID != "" {
		q.Set("dag_run_id", runID)
	}
	if taskID != "" {
		q.Set("task_id", taskID)
	}
	if tab != "" {
		q.Set("tab", tab)
	}
	u := strings.TrimRight(webserverURL, "/") + "/dags/" + url.PathEscape(dagID) + "/grid"
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// openBrowser is a variable so tests can stub it.
var openBrowser = func(u string) error {
	return exec.Command("open", u).Start()
}
