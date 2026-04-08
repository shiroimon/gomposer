package config

import (
	"os"
	"path/filepath"
	"testing"
)

const testTOML = `
[environments.qa]
name = "qa"
webserver_url = "https://qa.example.com"

[environments.prod]
name = "prod"
webserver_url = "https://prod.example.com"

[default]
environment = "qa"
`

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad(t *testing.T) {
	path := writeTempConfig(t, testTOML)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if len(cfg.Environments) != 2 {
		t.Fatalf("expected 2 environments, got %d", len(cfg.Environments))
	}

	qa := cfg.Environments["qa"]
	if qa.Name != "qa" {
		t.Errorf("expected qa name, got %q", qa.Name)
	}
	if qa.WebserverURL != "https://qa.example.com" {
		t.Errorf("unexpected webserver_url: %q", qa.WebserverURL)
	}

	if cfg.Default.Environment != "qa" {
		t.Errorf("expected default environment 'qa', got %q", cfg.Default.Environment)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/config.toml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_InvalidTOML(t *testing.T) {
	path := writeTempConfig(t, "not valid [[[toml")

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid TOML")
	}
}

func TestActiveEnv_Default(t *testing.T) {
	path := writeTempConfig(t, testTOML)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	env, err := cfg.ActiveEnv("")
	if err != nil {
		t.Fatalf("ActiveEnv failed: %v", err)
	}
	if env.Name != "qa" {
		t.Errorf("expected 'qa', got %q", env.Name)
	}
}

func TestActiveEnv_Explicit(t *testing.T) {
	path := writeTempConfig(t, testTOML)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	env, err := cfg.ActiveEnv("prod")
	if err != nil {
		t.Fatalf("ActiveEnv failed: %v", err)
	}
	if env.Name != "prod" {
		t.Errorf("expected 'prod', got %q", env.Name)
	}
}

func TestActiveEnv_NotFound(t *testing.T) {
	path := writeTempConfig(t, testTOML)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	_, err = cfg.ActiveEnv("staging")
	if err == nil {
		t.Fatal("expected error for unknown environment")
	}
}
