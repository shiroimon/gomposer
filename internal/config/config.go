package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Environment struct {
	Name         string `toml:"name"`
	WebserverURL string `toml:"webserver_url"`
	ColorTheme   string `toml:"color_theme"` // "red", "blue", "green", "yellow", "default"
}

type Config struct {
	Environments map[string]Environment `toml:"environments"`
	Default      struct {
		Environment string `toml:"environment"`
	} `toml:"default"`
	UI struct {
		RefreshIntervalSec int `toml:"refresh_interval_sec"` // 0 = disabled
	} `toml:"ui"`
}

// RefreshInterval returns the auto-refresh interval from config, or 0 if disabled.
func (c *Config) RefreshInterval() int {
	return c.UI.RefreshIntervalSec
}

// ActiveEnv returns the environment specified by envName,
// or the default environment if envName is empty.
func (c *Config) ActiveEnv(envName string) (Environment, error) {
	if envName == "" {
		envName = c.Default.Environment
	}
	env, ok := c.Environments[envName]
	if !ok {
		return Environment{}, fmt.Errorf("environment %q not found in config", envName)
	}
	return env, nil
}

// EnvNames returns the sorted list of environment keys.
func (c *Config) EnvNames() []string {
	names := make([]string, 0, len(c.Environments))
	for k := range c.Environments {
		names = append(names, k)
	}
	// simple sort
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[i] > names[j] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	return names
}

// DefaultConfigPaths returns the ordered list of config file paths to try.
func DefaultConfigPaths() []string {
	paths := []string{"config.toml"}

	// XDG_CONFIG_HOME or ~/.config
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			configDir = filepath.Join(home, ".config")
		}
	}
	if configDir != "" {
		paths = append(paths, filepath.Join(configDir, "gomposer", "config.toml"))
	}

	return paths
}

// Load reads a TOML config file from the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	return &cfg, nil
}

// LoadWithFallback tries the given path first, then falls back to default locations.
func LoadWithFallback(path string) (*Config, error) {
	// If an explicit path was given (not the default), only try that path
	if path != "" {
		return Load(path)
	}

	var lastErr error
	for _, p := range DefaultConfigPaths() {
		cfg, err := Load(p)
		if err == nil {
			return cfg, nil
		}
		lastErr = err
	}
	return nil, lastErr
}
