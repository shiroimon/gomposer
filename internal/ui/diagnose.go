package ui

import (
	"fmt"
	"strings"

	"github.com/shiroimon/gomposer/internal/api"
	"github.com/shiroimon/gomposer/internal/model"

	"github.com/charmbracelet/lipgloss"
)

var diagHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")).PaddingLeft(1)
var diagErrorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
var diagWarnStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
var diagOkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))

// DiagnosisResult holds the diagnosis output for display.
type DiagnosisResult struct {
	Lines []string
}

// RunDiagnosis performs a diagnostic scan of all DAGs.
func RunDiagnosis(ds api.DataSource) DiagnosisResult {
	dags, _ := ds.ListDAGs()
	var lines []string

	lines = append(lines, diagHeaderStyle.Render("=== Gomposer Diagnosis ==="))
	lines = append(lines, "")

	// 1. Failed DAG Runs
	var failedDAGs []model.DAG
	for _, dag := range dags {
		if dag.LastRunState == "failed" {
			failedDAGs = append(failedDAGs, dag)
		}
	}

	if len(failedDAGs) == 0 {
		lines = append(lines, diagOkStyle.Render("  No failed DAG Runs detected."))
	} else {
		lines = append(lines, diagErrorStyle.Render(fmt.Sprintf("  %d DAG(s) with failed last run:", len(failedDAGs))))
		lines = append(lines, "")
		for _, dag := range failedDAGs {
			lines = append(lines, diagErrorStyle.Render(fmt.Sprintf("  [FAILED] %s", dag.ID)))
			// Find root cause task
			runs := ds.ListDAGRuns(dag.ID)
			if len(runs) > 0 {
				latestRun := runs[len(runs)-1]
				tasks := ds.ListTaskInstances(dag.ID, latestRun.RunID)
				for _, task := range tasks {
					if task.State == "failed" {
						lines = append(lines, fmt.Sprintf("    Root cause: %s (try #%d)", task.TaskID, task.TryNumber))
						// Show first few lines of log
						log := ds.GetTaskLog(dag.ID, latestRun.RunID, task.TaskID, task.TryNumber)
						logLines := extractErrorLines(log)
						for _, l := range logLines {
							lines = append(lines, fmt.Sprintf("      %s", diagErrorStyle.Render(l)))
						}
						break
					}
				}
			}
			lines = append(lines, "")
		}
	}

	// 2. False success detection
	lines = append(lines, diagHeaderStyle.Render("--- False Success Detection ---"))
	lines = append(lines, "")
	falseSuccessCount := 0
	for _, dag := range dags {
		if dag.LastRunState == "success" {
			runs := ds.ListDAGRuns(dag.ID)
			if len(runs) == 0 {
				continue
			}
			latestRun := runs[len(runs)-1]
			tasks := ds.ListTaskInstances(dag.ID, latestRun.RunID)
			var problematic []string
			for _, t := range tasks {
				if t.State == "upstream_failed" || t.State == "skipped" {
					problematic = append(problematic, fmt.Sprintf("%s (%s)", t.TaskID, t.State))
				}
			}
			if len(problematic) > 0 {
				falseSuccessCount++
				lines = append(lines, diagWarnStyle.Render(fmt.Sprintf("  [FALSE SUCCESS] %s", dag.ID)))
				for _, p := range problematic {
					lines = append(lines, fmt.Sprintf("    - %s", diagWarnStyle.Render(p)))
				}
				lines = append(lines, "")
			}
		}
	}
	if falseSuccessCount == 0 {
		lines = append(lines, diagOkStyle.Render("  No false success detected."))
	}
	lines = append(lines, "")

	// 3. Summary
	lines = append(lines, diagHeaderStyle.Render("--- Summary ---"))
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  Total DAGs: %d", len(dags)))
	active := 0
	paused := 0
	for _, dag := range dags {
		if dag.IsPaused {
			paused++
		} else {
			active++
		}
	}
	lines = append(lines, fmt.Sprintf("  Active: %d, Paused: %d", active, paused))
	lines = append(lines, fmt.Sprintf("  Failed: %s", diagErrorStyle.Render(fmt.Sprintf("%d", len(failedDAGs)))))
	lines = append(lines, fmt.Sprintf("  False Success: %s", diagWarnStyle.Render(fmt.Sprintf("%d", falseSuccessCount))))
	lines = append(lines, "")

	return DiagnosisResult{Lines: lines}
}

func extractErrorLines(log string) []string {
	lines := strings.Split(log, "\n")
	var errors []string
	for _, line := range lines {
		if strings.Contains(line, "ERROR") || strings.Contains(line, "Traceback") || strings.Contains(line, "Exception") {
			errors = append(errors, strings.TrimSpace(line))
			if len(errors) >= 5 {
				break
			}
		}
	}
	return errors
}
