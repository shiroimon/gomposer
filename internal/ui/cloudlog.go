package ui

import (
	"fmt"
	"os/exec"
	"strings"
)

// FetchCloudLogs attempts to fetch logs from Cloud Logging when Airflow task log is empty.
// Uses gcloud logging read command. Returns the log content or an error message.
func FetchCloudLogs(dagID, taskID string) string {
	// Check if gcloud is available
	if _, err := exec.LookPath("gcloud"); err != nil {
		return "(gcloud CLI not found — Cloud Logging fallback unavailable)"
	}

	filter := fmt.Sprintf(
		`resource.type="cloud_composer_environment" AND `+
			`labels."workflow"="%s" AND `+
			`labels."task-id"="%s" AND `+
			`severity>=WARNING`,
		dagID, taskID,
	)

	cmd := exec.Command("gcloud", "logging", "read", filter,
		"--limit=50",
		"--format=value(timestamp,severity,textPayload)",
		"--freshness=7d",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("(Cloud Logging query failed: %v)\n%s", err, string(output))
	}

	result := strings.TrimSpace(string(output))
	if result == "" {
		return "(No relevant entries found in Cloud Logging)"
	}

	// Scan for known patterns
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

	header := "=== Cloud Logging Results ===\n"
	if len(findings) > 0 {
		header += strings.Join(findings, "\n") + "\n\n"
	}
	return header + result
}

// IsLogEmpty checks if an Airflow task log is effectively empty.
func IsLogEmpty(log string) bool {
	trimmed := strings.TrimSpace(log)
	return trimmed == "" ||
		trimmed == "(no log available)" ||
		strings.HasPrefix(trimmed, "(error fetching log:")
}
