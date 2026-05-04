# Backup-xd

TUI backup manager for databases and filesystems. Create, schedule, and manage backups with an interactive terminal interface. Built with Go and [Bubble Tea](https://github.com/charmbracelet/bubbletea).

## Quick Install

Supported platforms: Linux and macOS. On Windows, use WSL.

Recommended (installs to `~/.local/bin`):

```bash
curl -fsSL https://raw.githubusercontent.com/LFroesch/backup-xd/main/install.sh | bash
```

Or download a binary from [GitHub Releases](https://github.com/LFroesch/backup-xd/releases).

Or install with Go:

```bash
go install github.com/LFroesch/backup-xd@latest
```

Or build from source:

```bash
make install
```

Command:

```bash
backup-xd
```

There are no standalone subcommands yet. The current CLI is the interactive TUI launched by `backup-xd`.

## Features

- **Database backups** — PostgreSQL (pg_dump), MySQL (mysqldump), MongoDB (mongodump)
- **Filesystem backups** — Individual files and compressed directory archives
- **In-app scheduling** — Due jobs run automatically while `backup-xd` is open
- **Job management** — Create, edit, pause/resume, delete backup jobs
- **Global backup view** — Browse all backups across types
- **Cleanup** — Remove old backups based on retention
- **Restore** — Confirmed restore from the latest backup, including MySQL
- **Metadata tracking** — Timestamp, file size, duration saved with each backup

## Keybindings

### Main Menu

| Key | Action |
|-----|--------|
| `j/k`, `up/down` | Navigate |
| `enter` | Select menu item |
| `q` | Quit |

### Backup Management

| Key | Action |
|-----|--------|
| `a` | Add new backup job |
| `e` | Edit job |
| `enter` | Run backup now |
| `p` | Pause/resume job |
| `del` | Delete job |
| `ctrl+r` | Confirm and restore latest backup for selected job |
| `esc` | Back to menu |

### Global Backups

| Key | Action |
|-----|--------|
| `v` | View backup details |
| `d` | Delete selected backup |
| `esc` | Back to menu |

## Backup Types

| Type | Tool | Source |
|------|------|--------|
| `postgres` | `pg_dump` | Database name |
| `mysql` | `mysqldump` | Database name |
| `mongodb` | `mongodump` | Connection URI |
| `file` | `cp` | File path |
| `directory` | `tar` | Directory path |

## Configuration

### Config File

`~/.config/backup-xd/config.json` — backup job definitions

### Environment Variables

Database connections read from `~/.config/backup-xd/.backup-env`:

```bash
# PostgreSQL
export PGHOST=localhost
export PGUSER=postgres
export PGPASSWORD=your_password
export PGPORT=5432

# MySQL
export MYSQL_HOST=localhost
export MYSQL_USER=root
export MYSQL_PASSWORD=your_password
export MYSQL_PORT=3306

# MongoDB
export MONGO_URI=mongodb://user:pass@localhost:27017/dbname
```

### Backup Storage

```
~/backups/backup-xd/
├── postgres/<database>/<timestamp>/
├── mysql/<database>/<timestamp>/
├── mongodb/<job>/<timestamp>/
├── files/<filename>/<timestamp>/
└── directories/<directory>/<timestamp>/
```

Each backup includes a `metadata.json` with timestamp, size, duration, and job details.

## Schedule Options

| Schedule | Behavior |
|----------|----------|
| `1h` | Every hour |
| `24h` | Daily |
| `7d` | Weekly |
| `oneoff` | Manual-only job; never auto-scheduled |

Schedules are evaluated while the TUI is running. On each app tick, any active job whose `next_run` is due will start automatically. If the app is closed, missed runs are not executed until you open `backup-xd` again. Cron/systemd integration is still a backlog item if you want unattended scheduling while the app is not open.

## License

[AGPL-3.0](LICENSE)
