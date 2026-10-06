![Go](https://img.shields.io/badge/-GO-00ADD8.svg?logo=go&style=flat&color=F0F8FF&logoColor=00ADD8)
![Composer](https://img.shields.io/badge/-Cloud%20Composer-669DF6.svg?logo=googlecloudcomposer&style=flat&color=F0F8FF&logoColor=669DF6)

# <img src="img/logo.png" width=15% align="left" /> <div align="left">Gomposer <br> - Cloud Composer (Apache Airflow) TUI</div>

A terminal UI for [Google Cloud Composer](https://cloud.google.com/composer) (Apache Airflow), built for quick checks and light fixes.
Spot failed runs at a glance, read the logs, then clear, mark or re-trigger — all without leaving the terminal.
It is not a replacement for the Airflow web UI; for anything heavier, press `o` to jump there.

<img src="img/mock.png" width=120% />

## Requirements

- Go 1.24+
- For GCP connectivity: `gcloud` CLI installed and authenticated

## Installation

```bash
# Recommended — installs Go automatically if missing
mise install && mise run build

# If Go is already installed
go install github.com/shiroimon/gomposer/cmd/gomposer@latest
```

> [!TIP]
> If `$GOPATH/bin` is not in your PATH:
> ```bash
> echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc
> ```

<details>
<summary>Build from source</summary>

```bash
git clone https://github.com/shiroimon/gomposer.git && cd gomposer
make build        # → bin/gomposer
make install      # → $GOPATH/bin/gomposer
```

</details>

## Usage

```bash
gomposer              # Connect to Cloud Composer using config.toml
gomposer --mock       # Launch with mock data (no API connection required)
gomposer --env prod   # Specify an environment
gomposer --config /path/to/config.toml
```

## Configuration

Config is loaded from `~/.config/gomposer/config.toml` by default. Override with `--config`.

```bash
cp sample.config.toml ~/.config/gomposer/config.toml
```

Edit the file to match your setup:

```toml
[environments.qa]
webserver_url = "https://xxxxx-dot-us-central1.composer.googleusercontent.com"
color_theme = "blue"          # red / blue / green / yellow / default
gcp_project = "your-qa-project" # Cloud Logging project (required for H / L / log fallback)

[environments.prod]
webserver_url = "https://xxxxx-dot-us-central1.composer.googleusercontent.com"
color_theme = "red"
gcp_project = "your-prod-project"

[default]
environment = "qa"             # Default environment to connect

[ui]
refresh_interval_sec = 30      # Auto-refresh interval (0 = disabled)
```

## Key Bindings

The help bar shows the current tab's keys on the first row and the global keys pinned on the second row.
Keys in `( )` appear only when they apply.

- ▼ Global
> | Key                   | Action                                                                        |
> |-----------------------|-------------------------------------------------------------------------------|
> | `j` / `k` / `↑` / `↓` | Navigate / scroll                                                             |
> | `Enter`               | Drill down into selected item                                                 |
> | `Esc` / `Backspace`   | Go back (on DAGs: clear the filter)                                           |
> | `r`                   | Refresh data                                                                  |
> | `q`                   | Quit                                                                          |
> | `o`                   | Open the current item in the Airflow web UI (DAG / run graph, task, task log) |

```text
[DAGs]
/ filter │ t trigger │ p pause │ F fav │ ! problems │ I import err (│ E env) │ ? more
↑↓ move │ enter open │ r refresh │ esc back │ q quit │ o open in Airflow
```
- <details>
  <summary>DAGs</summary>

  > | Key     | Action                                                       |
  > |---------|--------------------------------------------------------------|
  > | `/`     | Filter by search                                             |
  > | `t`     | Trigger DAG                                                  |
  > | `p`     | Toggle pause / unpause (all selected DAGs if any)            |
  > | `F`     | Toggle `favorite`                                            |
  > | `!`     | Failed / retrying / running tasks across all DAGs (last 72h) |
  > | `I`     | DAG import errors                                            |
  > | `E`     | Switch environment (only with multiple environments)         |
  > | `?`     | List every key, including the ones below                     |
  > | `Space` | Select DAG for bulk pause (not shown in the help bar)        |
  > | `S`     | View DAG `source` code (not shown in the help bar)           |
  > | `G`     | View DAG dependency `graph` (not shown in the help bar)      |

  </details>

```text
[Runs]
s/f mark │ i info/conf │ ! problems
↑↓ move │ enter open │ r refresh │ esc back │ q quit │ o open in Airflow
```
- <details>
  <summary>Runs</summary>

  > | Key       | Action                                                       |
  > |-----------|--------------------------------------------------------------|
  > | `s` / `f` | Mark run as success / failed                                 |
  > | `i`       | Run type, conf and note                                      |
  > | `!`       | Failed / retrying / running tasks across all DAGs (last 72h) |

  </details>

```text
[Tasks]
c clear │ C clear all │ s/f mark │ x xcom │ H history │ ! problems
↑↓ move │ enter open │ r refresh │ esc back │ q quit │ o open in Airflow
```
- <details>
  <summary>Tasks</summary>

  > | Key       | Action                                                                                                |
  > |-----------|-------------------------------------------------------------------------------------------------------|
  > | `c`       | Clear task — toggle `d` downstream / `u` upstream / `o` only failed, `Enter` to preview, `y` to clear |
  > | `C`       | Clear all tasks in run (same preview)                                                                 |
  > | `s` / `f` | Mark task as success / failed                                                                         |
  > | `x`       | View [XCom]() entries                                                                                 |
  > | `H`       | Task history from Cloud Logging (last 30 days, one row per run)                                       |
  > | `!`       | Failed / retrying / running tasks across all DAGs (last 72h)                                          |

  </details>

```text
[Logs]
g/G top/bottom │ / search │ n/N next/prev │ L logs URL (│ [/] prev/next try)
↑↓ move │ enter open │ r refresh │ esc back │ q quit │ o open in Airflow
```

- <details>
  <summary>Logs</summary>

  > | Key       | Action                                                    |
  > |-----------|-----------------------------------------------------------|
  > | `g` / `G` | Jump to top / bottom                                      |
  > | `/`       | Search within logs                                        |
  > | `n` / `N` | Next / previous search result                             |
  > | `L`       | Copy Logs Explorer URL (Cloud Logging) for this run / try |
  > | `[` / `]` | Previous / next try (only when retried)                   |

  </details>

> [!NOTE]
> Cloud Logging
>
> When an Airflow task log is empty, gomposer reads the same run / try from Cloud Logging instead.
> `H` and `L` also use Cloud Logging. All of them query only the `gcp_project` of the current environment;
> if it is unset they refuse to run, so gcloud never falls back to its default project.

## License

MIT
