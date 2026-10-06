package ui

type Tab int

const (
	TabDAGs Tab = iota
	TabDAGRuns
	TabTaskInstances
	TabLogs
)

var tabNames = []string{"DAGs", "Runs", "Tasks", "Logs"}

func RenderTabs(active Tab) string {
	var tabs string
	for i, name := range tabNames {
		if Tab(i) == active {
			tabs += ActiveTabStyle.Render(name)
		} else {
			tabs += InactiveTabStyle.Render(name)
		}
	}
	return TabBarStyle.Render(tabs)
}
