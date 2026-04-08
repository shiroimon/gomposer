package api

import (
	"fmt"
	"os/exec"
	"strings"
)

// GetAccessToken retrieves a GCP access token using gcloud CLI.
func GetAccessToken() (string, error) {
	path, err := exec.LookPath("gcloud")
	if err != nil {
		return "", fmt.Errorf("gcloud CLI not found. Install it from https://cloud.google.com/sdk/docs/install")
	}

	cmd := exec.Command(path, "auth", "print-access-token")
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("gcloud auth failed: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("gcloud auth failed: %w", err)
	}

	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", fmt.Errorf("gcloud returned empty token. Run 'gcloud auth login' first")
	}

	return token, nil
}
