package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/shiroimon/gomposer/internal/api"
	"github.com/shiroimon/gomposer/internal/model"
)

func TestScrollOffset(t *testing.T) {
	cases := []struct {
		name                       string
		offset, cursor, n, h, want int
	}{
		{"fits", 0, 5, 8, 10, 0},
		{"cursor inside window", 3, 5, 30, 10, 3},
		{"cursor below window", 0, 12, 30, 10, 3},
		{"cursor above window", 10, 4, 30, 10, 4},
		{"list shrank", 25, 5, 12, 10, 2},
		{"unknown height", 7, 20, 30, 0, 0},
	}
	for _, c := range cases {
		if got := scrollOffset(c.offset, c.cursor, c.n, c.h); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestDAGRunListModel_WindowFollowsCursor(t *testing.T) {
	var runs []model.DAGRun
	for i := 0; i < 30; i++ {
		runs = append(runs, model.DAGRun{DagID: "d", RunID: fmt.Sprintf("run_%02d", i), State: "success"})
	}
	m := NewDAGRunListModel("d", runs, 0)
	m.SetSize(120)
	h := tableHeaderLines() + 5
	m.SetHeight(h)

	for i := 0; i < 12; i++ {
		m.CursorDown()
	}
	view := m.View()
	if got := lipgloss.Height(view); got != h {
		t.Fatalf("expected %d lines, got %d:\n%s", h, got, view)
	}
	if !strings.Contains(view, "run_12") || strings.Contains(view, "run_07") || strings.Contains(view, "run_13") {
		t.Errorf("window should end at the cursor (run_08..run_12):\n%s", view)
	}

	// Moving up inside the window must not scroll it.
	m.CursorUp()
	if view := m.View(); !strings.Contains(view, "run_08") || !strings.Contains(view, "run_12") {
		t.Errorf("window moved while cursor stayed inside it:\n%s", view)
	}
}

func TestAppModel_HeaderStaysPinned(t *testing.T) {
	var dags []model.DAG
	for i := 0; i < 60; i++ {
		dags = append(dags, model.DAG{ID: fmt.Sprintf("dag_%02d", i)})
	}
	var mdl tea.Model = NewAppModel(api.NewMockDataSource(), 0, nil, "mock")
	mdl, _ = mdl.Update(dagsLoadedMsg{dags: dags})
	mdl, _ = mdl.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	for i := 0; i < 45; i++ {
		mdl, _ = mdl.Update(tea.KeyMsg{Type: tea.KeyDown})
	}

	lines := strings.Split(mdl.View(), "\n")
	if len(lines) != 24 {
		t.Fatalf("expected exactly the terminal height (24), got %d lines", len(lines))
	}
	if !strings.Contains(lines[0], "Gomposer") {
		t.Errorf("title scrolled off: first line is %q", lines[0])
	}
	if !strings.Contains(strings.Join(lines, "\n"), "dag_45") {
		t.Error("selected row is not visible")
	}
}

func TestAppModel_EveryScreenFitsTerminal(t *testing.T) {
	const height = 18
	ds := api.NewMockDataSource()
	dags, err := ds.ListDAGs()
	if err != nil {
		t.Fatal(err)
	}
	var mdl tea.Model = NewAppModel(ds, 0, nil, "mock")
	mdl, _ = mdl.Update(dagsLoadedMsg{dags: dags})
	mdl, _ = mdl.Update(tea.WindowSizeMsg{Width: 100, Height: height})

	check := func(screen string) {
		t.Helper()
		lines := strings.Split(mdl.View(), "\n")
		if len(lines) != height || !strings.Contains(lines[0], "Gomposer") {
			t.Errorf("%s: %d lines, first line %q", screen, len(lines), lines[0])
		}
	}
	check("DAGs")
	for _, tab := range []Tab{TabDAGRuns, TabTaskInstances, TabLogs} {
		mdl = press(mdl, "enter")
		if got := mdl.(AppModel).tab; got != tab {
			t.Fatalf("expected tab %s, got %s", tabNames[tab], tabNames[got])
		}
		check(tabNames[tab])
	}
	mdl, _ = mdl.Update(tea.KeyMsg{Type: tea.KeyEsc})
	mdl, _ = mdl.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if mdl.(AppModel).overlay != overlayXCom {
		t.Fatal("XCom overlay did not open")
	}
	check("XCom overlay")
}

func TestAppModel_TinyPaneKeepsTitle(t *testing.T) {
	var mdl tea.Model = NewAppModel(api.NewMockDataSource(), 0, nil, "mock")
	mdl, _ = mdl.Update(dagsLoadedMsg{dags: []model.DAG{{ID: "a"}, {ID: "b"}, {ID: "c"}}})
	mdl, _ = mdl.Update(tea.WindowSizeMsg{Width: 100, Height: 5})

	lines := strings.Split(mdl.View(), "\n")
	if len(lines) > 5 || !strings.Contains(lines[0], "Gomposer") {
		t.Errorf("got %d lines, first line %q", len(lines), lines[0])
	}
}

func TestAppModel_HistoryOverlayLayout(t *testing.T) {
	const width, height = 95, 20
	am := NewAppModel(api.NewMockDataSource(), 0, nil, "mock")
	var mdl tea.Model = am
	mdl, _ = mdl.Update(dagsLoadedMsg{dags: []model.DAG{{ID: "a"}}})
	mdl, _ = mdl.Update(tea.WindowSizeMsg{Width: width, Height: height})

	am = mdl.(AppModel)
	var rows []HistoryRow
	rows = append(rows, HistoryRow{ExecutionDate: "2026-09-30T06:12:00+00:00", Try: "1", State: "failed",
		Detail: "airflow.exceptions.AirflowFailException: MDBのボリュームデータ一覧ページに目的のファイルが掲示されていません itemID=t000100000732"})
	for i := 0; i < 20; i++ {
		rows = append(rows, HistoryRow{ExecutionDate: fmt.Sprintf("2026-09-%02dT06:12:00+00:00", 29-i), Try: "1", State: "success", Detail: "2026-09-10"})
	}
	am.history = TaskHistory{Summary: "project: p · last 30 days · 21 runs", Rows: rows}
	am.overlayTitle = "History: dag › task"
	am.overlay = overlayHistory
	mdl, _ = am.Update(tea.WindowSizeMsg{Width: width, Height: height}) // renders for the width

	lines := strings.Split(mdl.View(), "\n")
	if len(lines) != height || !strings.Contains(lines[0], "Gomposer") {
		t.Fatalf("got %d lines, first %q", len(lines), lines[0])
	}
	view := strings.Join(lines, "\n")
	titleAt, summaryAt := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "History: dag › task") {
			titleAt = i
		}
		if strings.Contains(l, "project: p") {
			summaryAt = i
		}
	}
	if titleAt < 0 || summaryAt != titleAt+1 {
		t.Errorf("summary should sit on the line right below the title (title %d, summary %d):\n%s", titleAt, summaryAt, view)
	}
	if !strings.Contains(view, "itemID=t000100000732") {
		t.Errorf("long error was clipped instead of wrapped:\n%s", view)
	}
	for i := 0; i < 30; i++ {
		mdl, _ = mdl.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if scrolled := mdl.View(); !strings.Contains(scrolled, "LOGICAL DATE") || strings.Contains(scrolled, "2026-09-30T06:12") {
		t.Errorf("column header should stay pinned while rows scroll:\n%s", scrolled)
	}
	if scrolled := mdl.View(); !strings.Contains(scrolled, "2026-09-10T06:12") {
		t.Errorf("cursor moved to the last row but it is not on screen:\n%s", scrolled)
	}
	for i := 0; i < 30; i++ {
		mdl, _ = mdl.Update(tea.KeyMsg{Type: tea.KeyUp})
	}
	if back := mdl.View(); !strings.Contains(back, "itemID=t000100000732") {
		t.Errorf("back on the wrapped first row, all of its lines should be visible:\n%s", back)
	}
}

func TestAppModel_FilterLineSpacedLikeOtherSubtitles(t *testing.T) {
	am := NewAppModel(api.NewMockDataSource(), 0, nil, "mock")
	var mdl tea.Model = am
	mdl, _ = mdl.Update(dagsLoadedMsg{dags: []model.DAG{{ID: "update_drug_master"}, {ID: "other"}}})
	mdl, _ = mdl.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	am = mdl.(AppModel)
	am.dagList.ApplyFilter("drug")

	lines := strings.Split(ansi.Strip(am.View()), "\n")
	for i, l := range lines {
		if strings.Contains(l, `Filter: "drug" (1/2)`) {
			if i+2 >= len(lines) || strings.TrimSpace(lines[i+1]) != "" || !strings.Contains(lines[i+2], "DAG ID") {
				t.Errorf("expected a blank line between the filter line and the table header:\n%s", strings.Join(lines, "\n"))
			}
			return
		}
	}
	t.Errorf("filter line missing:\n%s", strings.Join(lines, "\n"))
}

func TestAppModel_DAGRefreshIsAsyncAndQuietOffTab(t *testing.T) {
	am := NewAppModel(api.NewMockDataSource(), 30, nil, "mock")
	var mdl tea.Model = am
	mdl, _ = mdl.Update(dagsLoadedMsg{dags: []model.DAG{{ID: "a"}}})
	am = mdl.(AppModel)
	am.tab = TabTaskInstances

	cmd := am.fetchDAGs(false)
	if cmd == nil || am.fetchDAGs(false) != nil {
		t.Fatal("expected one fetch and no second fetch while it is in flight")
	}
	msg, ok := cmd().(dagsRefreshedMsg)
	if !ok {
		t.Fatalf("expected dagsRefreshedMsg")
	}
	mdl, _ = am.Update(msg)
	am = mdl.(AppModel)
	if am.dagsFetching {
		t.Error("in-flight flag not cleared")
	}
	if len(am.dagList.DAGs()) <= 1 {
		t.Error("DAG list not updated from the background fetch")
	}
	if am.statusMsg != "" {
		t.Errorf("background refresh on another tab should be quiet, got %q", am.statusMsg)
	}
	if am.failedDAGCount() == 0 {
		t.Error("mock data has a failed DAG; the title count should pick it up off the DAGs tab")
	}

	stale := dagsRefreshedMsg{dags: nil, envName: "other"}
	mdl, _ = am.Update(stale)
	after := mdl.(AppModel)
	if len(after.dagList.DAGs()) == 0 {
		t.Error("a fetch from a previous environment must not overwrite the list")
	}
}
