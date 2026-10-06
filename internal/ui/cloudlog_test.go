package ui

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func stubGcloud(t *testing.T, out string, gotArgs *[]string) {
	t.Helper()
	orig := gcloudRun
	gcloudRun = func(args ...string) ([]byte, error) {
		if gotArgs != nil {
			*gotArgs = args
		}
		return []byte(out), nil
	}
	t.Cleanup(func() { gcloudRun = orig })
}

func TestTaskLogFilter(t *testing.T) {
	got := TaskLogFilter("dag_a", "task_1", "2026-09-30T04:12:00+00:00", 2)
	want := `resource.type="cloud_composer_environment"
labels.workflow="dag_a"
labels."task-id"="task_1"
labels."execution-date"="2026-09-30T04:12:00+00:00"
labels."try-number"="2"`
	if got != want {
		t.Errorf("unexpected filter:\n%s", got)
	}
	if strings.Contains(TaskLogFilter("d", "t", "", 0), "execution-date") {
		t.Error("expected no execution-date line when empty")
	}
}

func TestLogsExplorerURL(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	u := LogsExplorerURL("proj-a", TaskLogFilter("dag_a", "task_1", "", 0), start, end)

	if !strings.HasSuffix(u, ";startTime=2026-09-01T00:00:00Z;endTime=2026-10-02T00:00:00Z?project=proj-a") {
		t.Errorf("unexpected time range / project: %s", u)
	}
	if strings.Contains(u, "+") || strings.Contains(u, " ") {
		t.Errorf("query must not contain '+' or spaces: %s", u)
	}
	q := strings.TrimPrefix(strings.Split(u, ";")[1], "query=")
	decoded, err := url.PathUnescape(q)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != TaskLogFilter("dag_a", "task_1", "", 0) {
		t.Errorf("query does not round-trip: %q", decoded)
	}
}

func TestExecutionDateFromRunID(t *testing.T) {
	cases := map[string]string{
		"scheduled__2026-09-30T04:12:00+00:00":     "2026-09-30T04:12:00+00:00",
		"manual__2026-09-30T04:12:00.123456+00:00": "2026-09-30T04:12:00.123456+00:00",
		"my_custom_run":        "",
		"backfill__not-a-date": "",
	}
	for in, want := range cases {
		if got := ExecutionDateFromRunID(in); got != want {
			t.Errorf("%s: got %q, want %q", in, got, want)
		}
	}
}

func TestFetchCloudLogs_RequiresProject(t *testing.T) {
	called := false
	orig := gcloudRun
	gcloudRun = func(args ...string) ([]byte, error) { called = true; return nil, nil }
	t.Cleanup(func() { gcloudRun = orig })

	got := FetchCloudLogs("", "prod", "dag_a", "run_1", "task_1", 1)
	if called {
		t.Error("gcloud must not run without a project")
	}
	if !strings.Contains(got, "[environments.prod]") {
		t.Errorf("expected hint for the environment, got %q", got)
	}
}

func TestFetchCloudLogs_PassesProjectAndRunFilter(t *testing.T) {
	var args []string
	stubGcloud(t, `[{"timestamp":"2026-10-01T04:12:13Z","severity":"ERROR","textPayload":"boom","labels":{}}]`, &args)

	got := FetchCloudLogs("proj-a", "prod", "dag_a", "scheduled__2026-09-30T04:12:00+00:00", "task_1", 1)
	joined := strings.Join(args, " ")
	for _, want := range []string{"--project=proj-a", `labels."execution-date"="2026-09-30T04:12:00+00:00"`, `labels."try-number"="1"`, `timestamp>="2026-09-30T04:12:00Z"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("gcloud args missing %q: %s", want, joined)
		}
	}
	if !strings.Contains(got, "boom") || !strings.Contains(got, "proj-a") {
		t.Errorf("unexpected output: %q", got)
	}
}

func entry(ts, sev, execDate, try, text string) logEntry {
	t, _ := time.Parse(time.RFC3339, ts)
	return logEntry{Timestamp: t, Severity: sev, TextPayload: text,
		Labels: map[string]string{"execution-date": execDate, "try-number": try}}
}

func TestSummarizeHistory(t *testing.T) {
	d1, d2 := "2026-09-29T04:12:00+00:00", "2026-09-30T04:12:00+00:00"
	rows := SummarizeHistory([]logEntry{
		entry("2026-10-01T04:12:13Z", "ERROR", d2, "1", "Failed to execute job 1 for task t (msg; 123)\nTraceback (most recent call last):\n  File x\nValueError: second"),
		entry("2026-10-01T04:12:12Z", "ERROR", d2, "1", "Task failed with exception\nTraceback (most recent call last):\n  File x\nValueError: page has no file\n"),
		entry("2026-10-01T04:12:13Z", "INFO", d2, "1", "Immediate failure requested. Marking task as FAILED. dag_id=d"),
		entry("2026-09-30T04:12:13Z", "INFO", d1, "1", "Done. Returned value was: 2026-09-10"),
		entry("2026-09-30T04:12:14Z", "INFO", d1, "1", "Marking task as SUCCESS. dag_id=d"),
	})
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].ExecutionDate != d2 || rows[0].State != "failed" || rows[0].Detail != "ValueError: page has no file" {
		t.Errorf("unexpected newest row: %+v", rows[0])
	}
	if rows[1].State != "success" || rows[1].Detail != "2026-09-10" {
		t.Errorf("unexpected success row: %+v", rows[1])
	}
}

func TestFetchTaskHistory_RendersRowsAndURL(t *testing.T) {
	var args []string
	stubGcloud(t, `[
{"timestamp":"2026-09-30T04:12:14Z","severity":"INFO","textPayload":"Marking task as SUCCESS.","labels":{"execution-date":"2026-09-29T04:12:00+00:00","try-number":"1"}},
{"timestamp":"2026-09-30T04:12:13Z","severity":"INFO","textPayload":"Done. Returned value was: 2026-09-10","labels":{"execution-date":"2026-09-29T04:12:00+00:00","try-number":"1"}}
]`, &args)

	now := time.Date(2026, 10, 1, 5, 0, 0, 0, time.UTC)
	h := FetchTaskHistory("proj-a", "prod", "dag_a", "task_1", now)
	if content := renderRows(h, 120); !strings.Contains(content, "2026-09-10") || !strings.Contains(content, "success") {
		t.Errorf("unexpected content:\n%s", content)
	}
	if h.Summary != "project: proj-a · last 30 days · 1 runs" {
		t.Errorf("unexpected summary: %q", h.Summary)
	}
	if u := h.ExplorerURL; !strings.Contains(u, "?project=proj-a") || !strings.Contains(u, "startTime=2026-09-01T05:00:00Z") {
		t.Errorf("unexpected URL: %s", u)
	}
	if !strings.Contains(strings.Join(args, " "), "--project=proj-a") {
		t.Errorf("gcloud not pinned to project: %v", args)
	}
}

func TestTaskHistoryRender_WrapsLongErrorUnderItsColumn(t *testing.T) {
	msg := "airflow.exceptions.AirflowFailException: MDBのボリュームデータ一覧ページに目的のファイルが掲示されていません itemID=t000100000732"
	h := TaskHistory{Summary: "s", Rows: []HistoryRow{{ExecutionDate: "2026-09-30T06:12:00+00:00", Try: "1", State: "failed", Detail: msg}}}

	width := 120
	lines := strings.Split(ansi.Strip(strings.TrimRight(renderRows(h, width), "\n")), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected the error to wrap onto extra lines:\n%s", strings.Join(lines, "\n"))
	}
	var rebuilt []string
	for i, l := range lines {
		if w := lipgloss.Width(l); w > width {
			t.Errorf("line %d is %d columns wide, over %d", i, w, width)
		}
		if i > 0 && !strings.HasPrefix(l, strings.Repeat(" ", historyDetailCol)) {
			t.Errorf("continuation line not indented to the detail column: %q", l)
		}
		rebuilt = append(rebuilt, strings.TrimSpace(l[min(len(l), historyDetailCol):]))
	}
	if !strings.Contains(strings.Join(rebuilt, " "), "itemID=t000100000732") {
		t.Errorf("error text lost while wrapping:\n%s", strings.Join(lines, "\n"))
	}
}

func TestRenderStatus(t *testing.T) {
	if got := renderStatus("Refreshed"); !strings.Contains(got, "info: Refreshed") {
		t.Errorf("got %q", got)
	}
	if got := renderStatus("Error: boom"); !strings.Contains(got, "error: boom") {
		t.Errorf("got %q", got)
	}
	if got := renderStatus("Refresh failed: x"); !strings.Contains(got, "error: Refresh failed: x") {
		t.Errorf("got %q", got)
	}
}

func TestTaskHistoryHeader_PinsNoticeAndColumns(t *testing.T) {
	rows := []HistoryRow{{ExecutionDate: "d", Try: "1", State: "success", Detail: "v"}}
	h := TaskHistory{Summary: "s", Notice: "(hit the limit)", Rows: rows}
	header := ansi.Strip(h.Header())
	if !strings.HasPrefix(header, "(hit the limit)") || !strings.Contains(header, historyIndent+"LOGICAL DATE") {
		t.Errorf("unexpected header:\n%s", header)
	}
	if strings.Contains(renderRows(h, 120), "LOGICAL DATE") || strings.Contains(renderRows(h, 120), "hit the limit") {
		t.Error("header and notice must not scroll with the rows")
	}
	if got := ansi.Strip(renderRows(h, 120)); !strings.HasPrefix(got, historyIndent+"d ") {
		t.Errorf("rows not indented like the list tables: %q", got)
	}
	empty := TaskHistory{Notice: "(Cloud Logging disabled)"}
	if empty.Header() != "" || !strings.Contains(renderRows(empty, 80), "disabled") {
		t.Error("without rows the notice belongs in the body")
	}
}

func renderRows(h TaskHistory, width int) string {
	content, _ := h.Render(width, -1)
	return content
}

func TestTaskHistoryRender_HighlightsCursorRowAcrossWrappedLines(t *testing.T) {
	orig := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(orig) })

	long := strings.Repeat("error ", 30)
	h := TaskHistory{Summary: "s", Rows: []HistoryRow{
		{ExecutionDate: "a", Try: "1", State: "failed", Detail: long},
		{ExecutionDate: "b", Try: "1", State: "success", Detail: "v"},
	}}
	content, starts := h.Render(120, 0)
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(starts) != 2 || starts[0] != 0 || starts[1] < 2 {
		t.Fatalf("unexpected row starts %v for:\n%s", starts, content)
	}
	selected := SelectedRowStyle.Render("x")
	prefix := selected[:strings.Index(selected, "x")]
	if prefix == "" {
		t.Fatal("selected style emits no escape codes; the check below would be vacuous")
	}
	blockWidth := lipgloss.Width(lines[0])
	for i := 0; i < starts[1]; i++ {
		if !strings.HasPrefix(lines[i], prefix) || lipgloss.Width(lines[i]) != blockWidth {
			t.Errorf("line %d of the cursor row is not highlighted as one even block: %q", i, lines[i])
		}
	}

	short, _ := TaskHistory{Summary: "s", Rows: []HistoryRow{{ExecutionDate: "a", Try: "1", State: "success", Detail: "v"}}}.Render(120, 0)
	if got, want := lipgloss.Width(strings.TrimRight(short, "\n")), lipgloss.Width(historyHeaderText()); got != want {
		t.Errorf("short row highlight is %d wide, want the header width %d", got, want)
	}
	if strings.HasPrefix(lines[starts[1]], prefix) {
		t.Errorf("non-cursor row is highlighted: %q", lines[starts[1]])
	}
}
