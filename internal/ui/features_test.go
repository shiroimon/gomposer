package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shiroimon/gomposer/internal/api"
	"github.com/shiroimon/gomposer/internal/config"
	"github.com/shiroimon/gomposer/internal/model"
)

func press(mdl tea.Model, keys ...string) tea.Model {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		var cmd tea.Cmd
		mdl, cmd = mdl.Update(msg)
		// Opening the Runs tab loads in the background; deliver it so tests see the runs.
		if am, ok := mdl.(AppModel); ok && am.loadingRuns && cmd != nil {
			mdl, _ = mdl.Update(cmd())
		}
	}
	return mdl
}

func loadedApp(t *testing.T, cfg *config.Config) (tea.Model, *api.MockDataSource) {
	t.Helper()
	ds := api.NewMockDataSource()
	dags, err := ds.ListDAGs()
	if err != nil {
		t.Fatal(err)
	}
	var mdl tea.Model = NewAppModel(ds, 0, cfg, "qa")
	mdl, _ = mdl.Update(dagsLoadedMsg{dags: dags})
	mdl, _ = mdl.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	return mdl, ds
}

func TestAirflowGridURL(t *testing.T) {
	got := AirflowGridURL("https://af.example.com/", "my dag", "scheduled__2026-10-01T00:00:00+00:00", "grp.task", "logs")
	want := "https://af.example.com/dags/my%20dag/grid?dag_run_id=scheduled__2026-10-01T00%3A00%3A00%2B00%3A00&tab=logs&task_id=grp.task"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got := AirflowGridURL("https://af", "d", "", "", ""); got != "https://af/dags/d/grid" {
		t.Errorf("bare DAG URL: %s", got)
	}
}

func TestProblemList_SortsFailedFirst(t *testing.T) {
	now := time.Now()
	p := NewProblemListModel([]model.TaskInstance{
		{TaskID: "run_old", State: "running", StartDate: now.Add(-2 * time.Hour)},
		{TaskID: "retry", State: "up_for_retry", StartDate: now},
		{TaskID: "fail_old", State: "failed", StartDate: now.Add(-time.Hour)},
		{TaskID: "fail_new", State: "failed", StartDate: now},
	}, now.Add(-problemWindow))
	var got []string
	for _, t := range p.tasks {
		got = append(got, t.TaskID)
	}
	if strings.Join(got, ",") != "fail_new,fail_old,retry,run_old" {
		t.Errorf("order: %v", got)
	}
}

func TestOpenInAirflow_UsesWebserverURL(t *testing.T) {
	var opened string
	orig := openBrowser
	openBrowser = func(u string) error { opened = u; return nil }
	defer func() { openBrowser = orig }()

	cfg := &config.Config{Environments: map[string]config.Environment{"qa": {WebserverURL: "https://af"}}}
	mdl, _ := loadedApp(t, cfg)
	am := mdl.(AppModel)
	dag, _ := am.dagList.SelectedDAG()
	press(mdl, "o")
	if opened != "https://af/dags/"+dag.ID+"/grid?tab=graph" {
		t.Errorf("opened %q", opened)
	}

	opened = ""
	mdl, _ = loadedApp(t, nil)
	mdl = press(mdl, "o")
	if opened != "" || !strings.Contains(mdl.(AppModel).statusMsg, "webserver_url") {
		t.Errorf("without config: opened %q, status %q", opened, mdl.(AppModel).statusMsg)
	}
}

func TestProblems_EnterJumpsToTask(t *testing.T) {
	mdl, ds := loadedApp(t, nil)
	mdl = press(mdl, "!")
	am := mdl.(AppModel)
	if am.overlay != overlayProblems {
		t.Fatal("problems overlay did not open")
	}
	want, ok := am.problems.Selected()
	if !ok {
		t.Fatal("mock has no problem tasks")
	}
	if p, _ := ds.ListProblemTaskInstances(time.Now().Add(-problemWindow)); len(p) == 0 {
		t.Fatal("mock returned no problems")
	}
	am = press(mdl, "enter").(AppModel)
	if am.overlay != overlayNone || am.tab != TabTaskInstances {
		t.Fatalf("overlay %d tab %s", am.overlay, tabNames[am.tab])
	}
	got, _ := am.taskInstanceList.SelectedTask()
	if got.DagID != want.DagID || got.RunID != want.RunID || got.TaskID != want.TaskID {
		t.Errorf("jumped to %s/%s/%s, want %s/%s/%s", got.DagID, got.RunID, got.TaskID, want.DagID, want.RunID, want.TaskID)
	}
}

// openFailedRunTasks lands on the Tasks tab of a run that has a failed task, cursor on it.
func openFailedRunTasks(t *testing.T) (tea.Model, model.TaskInstance) {
	t.Helper()
	mdl, _ := loadedApp(t, nil)
	mdl = press(mdl, "!", "enter")
	am := mdl.(AppModel)
	task, _ := am.taskInstanceList.SelectedTask()
	if task.State != "failed" {
		t.Fatalf("expected a failed task, got %s", task.State)
	}
	return mdl, task
}

func TestClearDialog_PreviewThenClear(t *testing.T) {
	mdl, task := openFailedRunTasks(t)
	am := press(mdl, "c").(AppModel)
	if am.clear == nil || !am.clear.opts.IncludeDownstream {
		t.Fatal("single-task clear should start with downstream on")
	}
	if !strings.Contains(am.View(), "[d]ownstream:on") {
		t.Error("options line not shown")
	}
	am = press(am, "enter").(AppModel)
	if am.overlay != overlayClearPreview || len(am.clear.preview) == 0 || am.clear.preview[0] != task.TaskID {
		t.Fatalf("preview: overlay %d ids %v", am.overlay, am.clear.preview)
	}
	before, _ := am.taskInstanceList.SelectedTask()
	if before.State != "failed" {
		t.Fatal("dry run changed state")
	}

	am = press(am, "y").(AppModel)
	if am.clear != nil || am.overlay != overlayNone {
		t.Fatal("dialog not closed after clear")
	}
	after, _ := am.taskInstanceList.SelectedTask()
	if after.State == "failed" {
		t.Errorf("task still failed after clear; status %q", am.statusMsg)
	}
}

func TestClearDialog_EscCancels(t *testing.T) {
	mdl, _ := openFailedRunTasks(t)
	am := press(mdl, "c", "enter", "esc").(AppModel)
	if am.clear != nil || am.overlay != overlayNone {
		t.Fatal("esc on preview did not cancel")
	}
	got, _ := am.taskInstanceList.SelectedTask()
	if got.State != "failed" {
		t.Error("cancelled clear still changed state")
	}
}

func TestTaskMark_ConfirmSetsState(t *testing.T) {
	mdl, _ := openFailedRunTasks(t)
	am := press(mdl, "s", "n").(AppModel)
	if got, _ := am.taskInstanceList.SelectedTask(); got.State != "failed" {
		t.Fatal("declined mark changed state")
	}
	am = press(am, "s", "y").(AppModel)
	if got, _ := am.taskInstanceList.SelectedTask(); got.State != "success" {
		t.Errorf("state %s, status %q", got.State, am.statusMsg)
	}
}

func TestRunInfo_OverlayShowsType(t *testing.T) {
	mdl, _ := loadedApp(t, nil)
	am := press(mdl, "enter", "i").(AppModel)
	if am.overlay != overlayRunInfo || !strings.Contains(am.overlayContent, "scheduled") {
		t.Errorf("overlay %d content %q", am.overlay, am.overlayContent)
	}
}

func TestDAGsHelp_MoreListsHiddenKeys(t *testing.T) {
	mdl, _ := loadedApp(t, nil)
	view := mdl.View()
	for _, hidden := range []string{"space select", "S source", "G graph", "diagnose"} {
		if strings.Contains(view, hidden) {
			t.Errorf("help bar should not show %q", hidden)
		}
	}
	if !strings.Contains(view, "? more") {
		t.Error("help bar should offer ? more")
	}
	am := press(mdl, "?").(AppModel)
	if am.overlay != overlayKeys {
		t.Fatal("? did not open the key list")
	}
	for _, d := range []string{"select (for bulk pause)", "source", "graph", "filter", "open in Airflow"} {
		if !strings.Contains(am.overlayContent, d) {
			t.Errorf("key list missing %q", d)
		}
	}
	if am = press(am, "esc").(AppModel); am.overlay != overlayNone {
		t.Error("esc should close the key list")
	}
}
