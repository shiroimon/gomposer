package ui

import (
	"fmt"
	"strings"

	"gomposer/internal/model"

	"github.com/charmbracelet/lipgloss"
)

var graphNodeStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("69"))
var graphArrowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

// RenderDAGGraph renders an ASCII representation of the DAG task dependency graph.
func RenderDAGGraph(detail model.DAGDetail) string {
	if len(detail.Tasks) == 0 {
		return HelpStyle.Render("  No tasks found.")
	}

	// Build adjacency and find roots (tasks with no upstream)
	downstream := map[string][]string{}
	hasUpstream := map[string]bool{}
	for _, t := range detail.Tasks {
		downstream[t.TaskID] = t.DownstreamIDs
		for _, d := range t.DownstreamIDs {
			hasUpstream[d] = true
		}
	}

	var roots []string
	for _, t := range detail.Tasks {
		if !hasUpstream[t.TaskID] {
			roots = append(roots, t.TaskID)
		}
	}
	if len(roots) == 0 {
		// fallback: use first task
		roots = []string{detail.Tasks[0].TaskID}
	}

	// BFS to build layers
	layers := buildLayers(roots, downstream)

	// Render
	var sb strings.Builder
	sb.WriteString(HeaderStyle.Render(fmt.Sprintf("  DAG: %s", detail.DagID)))
	sb.WriteString("\n")
	sb.WriteString(HelpStyle.Render(fmt.Sprintf("  File: %s", detail.FileLoc)))
	sb.WriteString("\n\n")

	for i, layer := range layers {
		// Render nodes in this layer
		var nodes []string
		for _, taskID := range layer {
			nodes = append(nodes, graphNodeStyle.Render(fmt.Sprintf("[ %s ]", taskID)))
		}
		sb.WriteString("  ")
		sb.WriteString(strings.Join(nodes, "   "))
		sb.WriteString("\n")

		// Render arrows to next layer
		if i < len(layers)-1 {
			sb.WriteString("  ")
			for j, taskID := range layer {
				ds := downstream[taskID]
				if len(ds) > 0 {
					if j > 0 {
						sb.WriteString("   ")
					}
					arrows := make([]string, len(ds))
					for k := range ds {
						arrows[k] = "|"
					}
					sb.WriteString(graphArrowStyle.Render(strings.Join(arrows, " ")))
				}
			}
			sb.WriteString("\n  ")
			for j, taskID := range layer {
				ds := downstream[taskID]
				if len(ds) > 0 {
					if j > 0 {
						sb.WriteString("   ")
					}
					arrows := make([]string, len(ds))
					for k := range ds {
						arrows[k] = "v"
					}
					sb.WriteString(graphArrowStyle.Render(strings.Join(arrows, " ")))
				}
			}
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

func buildLayers(roots []string, downstream map[string][]string) [][]string {
	visited := map[string]bool{}
	var layers [][]string
	current := roots

	for len(current) > 0 {
		layer := make([]string, 0, len(current))
		for _, id := range current {
			if !visited[id] {
				visited[id] = true
				layer = append(layer, id)
			}
		}
		if len(layer) == 0 {
			break
		}
		layers = append(layers, layer)

		var next []string
		seen := map[string]bool{}
		for _, id := range layer {
			for _, d := range downstream[id] {
				if !visited[d] && !seen[d] {
					next = append(next, d)
					seen[d] = true
				}
			}
		}
		current = next
	}

	return layers
}
