package ui

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// HistoryDays is how far back the task history view looks in Cloud Logging.
const HistoryDays = 30

// historyLimit caps the number of entries fetched for the history view.
const historyLimit = 5000

// gcloudRun executes gcloud and returns combined output. Replaced in tests.
var gcloudRun = func(args ...string) ([]byte, error) {
	if _, err := exec.LookPath("gcloud"); err != nil {
		return nil, fmt.Errorf("gcloud CLI not found")
	}
	return exec.Command("gcloud", args...).CombinedOutput()
}

// logEntry is the subset of a Cloud Logging entry gomposer reads.
type logEntry struct {
	Timestamp   time.Time         `json:"timestamp"`
	Severity    string            `json:"severity"`
	TextPayload string            `json:"textPayload"`
	Labels      map[string]string `json:"labels"`
}

// TaskLogFilter builds the Cloud Logging filter for one task.
// executionDate and tryNumber narrow it to a single run/try when non-empty / > 0.
func TaskLogFilter(dagID, taskID, executionDate string, tryNumber int) string {
	lines := []string{
		`resource.type="cloud_composer_environment"`,
		fmt.Sprintf(`labels.workflow="%s"`, dagID),
		fmt.Sprintf(`labels."task-id"="%s"`, taskID),
	}
	if executionDate != "" {
		lines = append(lines, fmt.Sprintf(`labels."execution-date"="%s"`, executionDate))
	}
	if tryNumber > 0 {
		lines = append(lines, fmt.Sprintf(`labels."try-number"="%d"`, tryNumber))
	}
	return strings.Join(lines, "\n")
}

// LogsExplorerURL returns a Cloud Console Logs Explorer URL with the query and time range pre-filled.
func LogsExplorerURL(project, filter string, start, end time.Time) string {
	q := strings.ReplaceAll(url.QueryEscape(filter), "+", "%20")
	return fmt.Sprintf("https://console.cloud.google.com/logs/query;query=%s;startTime=%s;endTime=%s?project=%s",
		q, start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339), url.QueryEscape(project))
}

// ExecutionDateFromRunID extracts the logical date that Composer puts in the
// "execution-date" log label, e.g. scheduled__2026-09-30T04:12:00+00:00.
func ExecutionDateFromRunID(runID string) string {
	_, suffix, ok := strings.Cut(runID, "__")
	if !ok {
		return ""
	}
	if _, err := time.Parse(time.RFC3339Nano, suffix); err != nil {
		return ""
	}
	return suffix
}

// readLogs runs gcloud logging read against the given project and parses the JSON output.
func readLogs(project, filter string, limit int, order string) ([]logEntry, error) {
	out, err := gcloudRun("logging", "read", filter,
		"--project="+project,
		"--limit="+strconv.Itoa(limit),
		"--order="+order,
		"--format=json",
	)
	if err != nil {
		return nil, fmt.Errorf("%v\n%s", err, strings.TrimSpace(string(out)))
	}
	var entries []logEntry
	if err := json.Unmarshal(out, &entries); err != nil {
		return nil, fmt.Errorf("parsing gcloud output: %v", err)
	}
	return entries, nil
}

func missingProjectMsg(envName string) string {
	if envName == "" || envName == "mock" {
		return "(Cloud Logging is unavailable in mock mode)"
	}
	// gcloud would silently fall back to its default project, which may be another environment.
	return fmt.Sprintf("(Cloud Logging disabled: set gcp_project for [environments.%s] in config.toml)", envName)
}

// FetchCloudLogs fetches a task run's logs from Cloud Logging when the Airflow task log is empty.
func FetchCloudLogs(project, envName, dagID, runID, taskID string, tryNumber int) string {
	if project == "" {
		return missingProjectMsg(envName)
	}

	execDate := ExecutionDateFromRunID(runID)
	since := time.Now().AddDate(0, 0, -7)
	if t, err := time.Parse(time.RFC3339Nano, execDate); err == nil {
		since = t
	}
	filter := TaskLogFilter(dagID, taskID, execDate, tryNumber) +
		fmt.Sprintf("\ntimestamp>=\"%s\"", since.UTC().Format(time.RFC3339))

	entries, err := readLogs(project, filter, 1000, "asc")
	if err != nil {
		return fmt.Sprintf("(Cloud Logging query failed: %v)", err)
	}
	if len(entries) == 0 {
		return fmt.Sprintf("(No relevant entries found in Cloud Logging project %s)", project)
	}

	var sb strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&sb, "%s %-7s %s\n", e.Timestamp.Local().Format("2006-01-02 15:04:05"), e.Severity, e.TextPayload)
	}
	result := sb.String()

	var findings []string
	if strings.Contains(result, "Celery") || strings.Contains(result, "timeout") {
		findings = append(findings, "[PATTERN] Celery timeout detected")
	}
	if strings.Contains(result, "evict") || strings.Contains(result, "OOMKilled") {
		findings = append(findings, "[PATTERN] Worker eviction / OOM detected")
	}
	if strings.Contains(result, "ResourceExhausted") {
		findings = append(findings, "[PATTERN] Resource exhaustion detected")
	}

	header := fmt.Sprintf("=== Cloud Logging Results (%s) ===\n", project)
	if len(findings) > 0 {
		header += strings.Join(findings, "\n") + "\n\n"
	}
	return header + result
}

// HistoryRow summarizes one task try found in Cloud Logging.
type HistoryRow struct {
	ExecutionDate string
	Try           string
	EndedAt       time.Time
	State         string // success, failed, up_for_retry, or "?" when no outcome line was logged
	Detail        string // returned value on success, error message on failure
}

// SummarizeHistory groups log entries by run and try, newest first.
func SummarizeHistory(entries []logEntry) []HistoryRow {
	type key struct{ execDate, try string }
	rows := map[key]*HistoryRow{}
	errText := map[key]string{}

	sorted := append([]logEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Timestamp.Before(sorted[j].Timestamp) })

	for _, e := range sorted {
		k := key{e.Labels["execution-date"], e.Labels["try-number"]}
		r, ok := rows[k]
		if !ok {
			r = &HistoryRow{ExecutionDate: k.execDate, Try: k.try, State: "?"}
			rows[k] = r
		}
		if e.Timestamp.After(r.EndedAt) {
			r.EndedAt = e.Timestamp
		}
		text := e.TextPayload
		switch {
		case strings.Contains(text, "Marking task as SUCCESS"):
			r.State = "success"
		case strings.Contains(text, "Marking task as FAILED"):
			r.State = "failed"
		case strings.Contains(text, "Marking task as UP_FOR_RETRY"):
			r.State = "up_for_retry"
		case strings.Contains(text, "Returned value was:"):
			_, v, _ := strings.Cut(text, "Returned value was:")
			r.Detail = strings.TrimSpace(v)
		case e.Severity == "ERROR" || e.Severity == "CRITICAL":
			// Prefer the traceback: its last line is the exception message.
			if errText[k] == "" || strings.HasPrefix(text, "Task failed with exception") {
				errText[k] = lastNonEmptyLine(text)
			}
		}
	}

	out := make([]HistoryRow, 0, len(rows))
	for k, r := range rows {
		if r.State != "success" && errText[k] != "" {
			r.Detail = errText[k]
		}
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ExecutionDate != out[j].ExecutionDate {
			return out[i].ExecutionDate > out[j].ExecutionDate
		}
		return out[i].Try > out[j].Try
	})
	return out
}

func lastNonEmptyLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}

// TaskHistory is a task's per-run outcomes over the last HistoryDays, fetched from Cloud Logging.
type TaskHistory struct {
	Summary     string // project, range and run count; empty when the query did not run
	Notice      string // error or truncation warning shown above the table
	Rows        []HistoryRow
	ExplorerURL string
}

// FetchTaskHistory queries Cloud Logging for a task's outcomes over the last HistoryDays.
func FetchTaskHistory(project, envName, dagID, taskID string, now time.Time) TaskHistory {
	if project == "" {
		return TaskHistory{Notice: missingProjectMsg(envName)}
	}
	start := now.AddDate(0, 0, -HistoryDays)
	base := TaskLogFilter(dagID, taskID, "", 0)
	h := TaskHistory{ExplorerURL: LogsExplorerURL(project, base, start, now)}

	// Only outcome lines are needed; full logs of a chatty task would hit the limit.
	filter := base + fmt.Sprintf("\ntimestamp>=\"%s\"\n", start.UTC().Format(time.RFC3339)) +
		`(textPayload:"Marking task as" OR textPayload:"Returned value was" OR severity>=ERROR)`
	entries, err := readLogs(project, filter, historyLimit, "desc")
	if err != nil {
		h.Notice = fmt.Sprintf("(Cloud Logging query failed: %v)", err)
		return h
	}

	h.Rows = SummarizeHistory(entries)
	h.Summary = fmt.Sprintf("project: %s · last %d days · %d runs", project, HistoryDays, len(h.Rows))
	if len(entries) >= historyLimit {
		h.Notice = fmt.Sprintf("(hit the %d-entry limit — older runs may be missing)", historyLimit)
	}
	return h
}

// historyIndent matches the left margin of the list tables so the overlay lines up with them.
const historyIndent = "  "

// historyDetailCol is where the RETURNED / ERROR column starts; wrapped lines are indented to it.
const historyDetailCol = len(historyIndent) + 26 + 2 + 16 + 2 + 3 + 2 + 12 + 2

const historyRowFormat = historyIndent + "%-26s  %-16s  %-3s  %s  %s"

func historyHeaderText() string {
	return fmt.Sprintf(historyRowFormat, "LOGICAL DATE", "ENDED (LOCAL)", "TRY", fmt.Sprintf("%-12s", "STATE"), "RETURNED / ERROR")
}

// Header is the pinned part above the rows: the notice, if any, and the styled column header.
// Empty when there are no rows; Render then shows the notice itself.
func (h TaskHistory) Header() string {
	if len(h.Rows) == 0 {
		return ""
	}
	header := TableHeaderStyle.Render(historyHeaderText())
	if h.Notice != "" {
		return h.Notice + "\n\n" + header
	}
	return header
}

// Render lays out the history rows for the given width, wrapping long errors under their column
// and highlighting the cursor row. rowStarts[i] is the first line of row i, for scrolling to it.
func (h TaskHistory) Render(width, cursor int) (content string, rowStarts []int) {
	if len(h.Rows) == 0 {
		var sb strings.Builder
		if h.Notice != "" {
			sb.WriteString(h.Notice + "\n\n")
		}
		if h.Summary != "" {
			sb.WriteString("No runs found in Cloud Logging.\n")
		}
		return sb.String(), nil
	}
	var lines []string
	indent := strings.Repeat(" ", historyDetailCol)
	for i, r := range h.Rows {
		rowStarts = append(rowStarts, len(lines))
		detail := strings.ReplaceAll(wrapText(r.Detail, width-historyDetailCol), "\n", "\n"+indent)
		ended := r.EndedAt.Local().Format("2006-01-02 15:04")
		state := fmt.Sprintf("%-12s", r.State)
		if i != cursor {
			lines = append(lines, strings.Split(fmt.Sprintf(historyRowFormat, r.ExecutionDate, ended, r.Try, StateStyle(r.State).Render(state), detail), "\n")...)
			continue
		}
		// Highlight a block as wide as the row's text (or the header), like the list tables, not the whole terminal.
		rowLines := strings.Split(fmt.Sprintf(historyRowFormat, r.ExecutionDate, ended, r.Try, state, detail), "\n")
		blockWidth := lipgloss.Width(historyHeaderText())
		for _, l := range rowLines {
			blockWidth = max(blockWidth, lipgloss.Width(l))
		}
		for _, l := range rowLines {
			lines = append(lines, SelectedRowStyle.Render(l+strings.Repeat(" ", blockWidth-lipgloss.Width(l))))
		}
	}
	return strings.Join(lines, "\n") + "\n", rowStarts
}

// wrapText wraps s to width display columns (wide characters count as 2). width < 20 leaves it unwrapped.
func wrapText(s string, width int) string {
	if width < 20 || lipgloss.Width(s) <= width {
		return s
	}
	lines := strings.Split(lipgloss.NewStyle().Width(width).Render(s), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n")
}

// IsLogEmpty checks if an Airflow task log is effectively empty.
func IsLogEmpty(log string) bool {
	trimmed := strings.TrimSpace(log)
	return trimmed == "" ||
		trimmed == "(no log available)" ||
		strings.HasPrefix(trimmed, "(error fetching log:")
}

// copyToClipboard copies text via pbcopy (macOS). Best-effort.
func copyToClipboard(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
