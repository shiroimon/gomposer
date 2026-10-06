package api

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
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

// tokenCache reuses one access token across requests. Fetching a token spawns
// gcloud, which costs far more than the API call it authorizes.
type tokenCache struct {
	mu      sync.Mutex
	fetch   func() (string, error)
	ttl     time.Duration
	token   string
	expires time.Time
	now     func() time.Time
}

func newTokenCache(fetch func() (string, error), ttl time.Duration) *tokenCache {
	return &tokenCache{fetch: fetch, ttl: ttl, now: time.Now}
}

func (c *tokenCache) Get() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && c.now().Before(c.expires) {
		return c.token, nil
	}
	token, err := c.fetch()
	if err != nil {
		return "", err
	}
	c.token = token
	c.expires = c.now().Add(c.ttl)
	return token, nil
}

func (c *tokenCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = ""
}

// gcloud access tokens live for 1 hour; refresh well before that.
// Shared across clients so switching environments keeps the token.
var sharedToken = newTokenCache(GetAccessToken, 50*time.Minute)
