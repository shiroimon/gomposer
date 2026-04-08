package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"gomposer/internal/api"
	"gomposer/internal/config"
	"gomposer/internal/ui"
)

// Version is set at build time via -ldflags.
var Version = "dev"

func main() {
	mock := flag.Bool("mock", false, "Use mock data instead of API")
	env := flag.String("env", "", "Environment name (overrides config default)")
	configPath := flag.String("config", "", "Path to config file (default: config.toml, then ~/.config/gomposer/config.toml)")
	version := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *version {
		fmt.Printf("gomposer %s\n", Version)
		os.Exit(0)
	}

	var ds api.DataSource
	var refreshIntervalSec int
	var cfg *config.Config
	var envName string

	if *mock {
		ds = api.NewMockDataSource()
		envName = "mock"
	} else {
		var err error
		cfg, err = config.LoadWithFallback(*configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			fmt.Fprintf(os.Stderr, "Config file locations: config.toml, ~/.config/gomposer/config.toml\n")
			fmt.Fprintf(os.Stderr, "Use --mock to run with mock data\n")
			os.Exit(1)
		}

		envName = *env
		if envName == "" {
			envName = cfg.Default.Environment
		}

		activeEnv, err := cfg.ActiveEnv(envName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		ds = api.NewAirflowClient(activeEnv.WebserverURL)
		refreshIntervalSec = cfg.RefreshInterval()
	}

	m := ui.NewAppModel(ds, refreshIntervalSec, cfg, envName)

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
