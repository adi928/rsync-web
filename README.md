# rsync-web

A web-based dashboard for scheduling and monitoring rsync backups over SSH. Configure your backup source, destination, and SSH key directly from the browser, then let the built-in scheduler handle the rest.

## Features

- **Web UI configuration** — set source path, remote host, remote path, and SSH key from the dashboard (no need to edit config files for transfer settings)
- **Scheduled backups** — cron-based scheduling with configurable expressions
- **Live dashboard** — real-time status updates via htmx (no full page reloads)
- **Backup history** — tracks all runs with status, duration, and exit codes
- **Log viewer** — view rsync output for any backup run directly in the browser
- **Remote path check** — warns if the remote destination already contains files before the first backup
- **Resume support** — uses `--partial` so interrupted transfers resume where they left off
- **Partial transfer warnings** — distinguishes between full failures and partial transfers (exit codes 23/24)
- **Log rotation** — automatically prunes old log files

## Quick Start

### Prerequisites

- Go 1.21+
- `rsync` installed locally
- SSH access to the remote backup server with a passphrase-less key

### Build and Run

```bash
go build -o rsync-web .
./rsync-web --config config.yaml
```

The dashboard will be available at `http://localhost:8090` (or whatever `listen_addr` is set to in your config).

### First-Time Setup

1. Copy the example config:
   ```bash
   cp config.example.yaml config.yaml
   ```

2. Edit `config.yaml` to set your schedule and server preferences (transfer settings are configured via the web UI):
   ```yaml
   schedule: "0 3 * * *"      # daily at 3:00 AM
   listen_addr: ":8090"
   log_dir: ./logs
   max_log_files: 30
   bandwidth_limit: 0         # KB/s, 0 = unlimited
   ```

3. Start the server and open the dashboard in your browser. You'll be prompted to enter:
   - **Source Path** — local directory or file to back up
   - **Remote Host** — SSH destination (`user@host`)
   - **Remote Path** — directory on the remote server
   - **SSH Key Path** — path to the private key (must have no passphrase)

4. Click **Save Settings**, then **Run Backup Now** to trigger your first sync.

## Configuration

### config.yaml

The config file handles server and scheduling settings:

| Field | Default | Description |
|-------|---------|-------------|
| `schedule` | *(required)* | Cron expression for automatic backups |
| `listen_addr` | `:8090` | Address and port for the web dashboard |
| `log_dir` | `./logs` | Directory to store backup log files |
| `max_log_files` | `30` | Maximum number of log files to keep |
| `bandwidth_limit` | `0` | Bandwidth limit in KB/s (0 = unlimited) |

Transfer settings (`source_path`, `remote_host`, `remote_path`, `ssh_key_path`) can also be set in the config file, but are primarily managed through the web UI. Settings entered via the UI are persisted to `settings.json` in the log directory.

### SSH Key Setup

The SSH key **must not** have a passphrase since backups run unattended. Create a dedicated key:

```bash
ssh-keygen -t ed25519 -f ~/.ssh/rsync-backup -N "" -C "rsync-backup"
```

On the remote server, restrict the key to rsync-only access in `~/.ssh/authorized_keys`:

```
command="rsync --server -vlogDtprze.iLsfxCIvu --delete --partial . /backups/path/",no-port-forwarding,no-X11-forwarding,no-agent-forwarding,no-pty ssh-ed25519 AAAA... rsync-backup
```

## API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/` | GET | Dashboard page |
| `/api/status` | GET | Current status as JSON |
| `/api/backup` | POST | Trigger a backup |
| `/api/history` | GET | Backup history as JSON |
| `/api/logs/{file}` | GET | View a specific log file |
| `/api/settings` | GET | Current transfer settings as JSON |
| `/api/settings` | POST | Update transfer settings |
| `/api/remote-check` | GET | Check if remote path has existing files |

## Development

### Run Tests

```bash
go test ./... -v
```

### Project Structure

```
.
├── main.go           # Entry point — wires up config, executor, scheduler, HTTP server
├── config.go         # Config struct, YAML loading, transfer settings persistence
├── backup.go         # BackupExecutor — runs rsync, manages history and logs
├── handlers.go       # HTTP handlers — dashboard, API, htmx fragments, settings
├── scheduler.go      # Cron-based backup scheduler
├── templates/
│   └── index.html    # HTML template with htmx-powered dashboard
├── static/
│   └── style.css     # CSS with dark/light mode support
├── config.yaml       # Your configuration (gitignored)
├── config.example.yaml
└── logs/             # Backup logs and history (gitignored)
    ├── history.json
    └── settings.json
```
