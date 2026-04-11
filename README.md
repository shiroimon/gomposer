![Go](https://img.shields.io/badge/-GO-00ADD8.svg?logo=go&style=flat&color=F0F8FF&logoColor=00ADD8)
![Composer](https://img.shields.io/badge/-Cloud%20Composer-669DF6.svg?logo=googlecloudcomposer&style=flat&color=F0F8FF&logoColor=669DF6)

# <img src="img/logo.png" width=15% align="left" /> <div align="left">Gomposer <br> - Cloud Composer (Apache Airflow) TUI</div>

A terminal UI for [Google Cloud Composer](https://cloud.google.com/composer) (Apache Airflow). Browse DAGs, drill into runs and task instances, read logs, trigger DAGs, and switch environments — all without leaving the terminal.

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

[environments.prod]
webserver_url = "https://xxxxx-dot-us-central1.composer.googleusercontent.com"
color_theme = "red"

[default]
environment = "qa"             # Default environment to connect

[ui]
refresh_interval_sec = 30      # Auto-refresh interval (0 = disabled)
```

## Key Bindings

<details>
<summary>Global</summary>

| Key | Action |
|-----|--------|
| `j` / `k` / `↑` / `↓` | Navigate / scroll |
| `Enter` | Drill down into selected item |
| `Esc` / `Backspace` | Go back |
| `r` | Refresh data |
| `q` | Quit |

</details>

<details>
<summary>DAG List</summary>

| Key | Action |
|-----|--------|
| `/` | Filter by search |
| `t` | Trigger DAG |
| `p` | Toggle pause / unpause |
| `Space` | Select DAG (multi-select) |
| `F` | Toggle favorite |
| `S` | View DAG source code |
| `G` | View DAG dependency graph |
| `d` | Failure diagnosis report |
| `E` | Switch environment |

</details>

<details>
<summary>DAG Runs</summary>

| Key | Action |
|-----|--------|
| `s` / `f` | Mark run as success / failed |

</details>

<details>
<summary>Task Instances</summary>

| Key | Action |
|-----|--------|
| `c` | Clear task |
| `C` | Clear all tasks in run |
| `x` | View [XCom]() entries |

</details>

<details>
<summary>Log Viewer</summary>

| Key | Action |
|-----|--------|
| `g` / `G` | Jump to top / bottom |
| `/` | Search within logs |
| `n` / `N` | Next / previous search result |

</details>

## License

MIT
