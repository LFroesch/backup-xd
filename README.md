# Backup-xd

TUI backup manager for databases and filesystems. Create, schedule, and manage backups with an interactive terminal interface. Built with Go and [Bubble Tea](https://github.com/charmbracelet/bubbletea).

## Quick Install

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
## Install

```bash
go install github.com/LFroesch/backup-xd@latest
```

Or build from source:

```bash
make install
```

## Usage

```bash
backup-xd
```

## Features

- **Database backups** — PostgreSQL (pg_dump), MySQL (mysqldump), MongoDB (mongodump)
- **Filesystem backups** — Individual files and compressed directory archives
- **Scheduling** — Recurring intervals (1h, 24h, 7d) or one-off
- **Job management** — Create, edit, pause/resume, delete backup jobs
- **Global backup view** — Browse all backups across types
- **Cleanup** — Remove old backups based on retention
- **Restore** — Quick restore from latest backup
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
| `d` | Delete job |
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

# MongoDB
export MONGO_URI=mongodb://user:pass@localhost:27017/dbname
```

### Backup Storage

```
~/backups/backup-xd/
├── postgres/
├── mysql/
├── mongodb/
├── files/
└── directories/
```

Each backup includes a `metadata.json` with timestamp, size, duration, and job details.

## Schedule Options

| Schedule | Behavior |
|----------|----------|
| `1h` | Every hour |
| `24h` | Daily |
| `7d` | Weekly |
| `oneoff` | Run once, then mark completed |

## License

[AGPL-3.0](LICENSE)
