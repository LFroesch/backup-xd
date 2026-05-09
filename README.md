# backup-xd

Terminal backup manager for local databases and filesystem targets. `backup-xd` lets you define jobs, run them on demand, review backup history, clean up old archives, and restore the latest backup from one TUI.

## Install

Supported platforms: Linux and macOS. On Windows, use WSL.

Recommended:

```bash
curl -fsSL https://raw.githubusercontent.com/LFroesch/backup-xd/main/install.sh | bash
```

Other options:

```bash
go install github.com/LFroesch/backup-xd@latest
make install
```

Then run:

```bash
backup-xd
backup-xd --version
```

## What It Covers

- PostgreSQL backups via `pg_dump`
- MySQL backups via `mysqldump`
- MongoDB backups via `mongodump`
- File copies
- Directory archives via `tar`

## How It Works

- Jobs are stored in `~/.config/backup-xd/config.json`
- Database credentials are read from `~/.config/backup-xd/.backup-env`
- Backups are written under `~/backups/backup-xd/`
- Each backup includes `metadata.json` with timestamp, size, duration, and job details

Example env file:

```bash
export PGHOST=localhost
export PGUSER=postgres
export PGPASSWORD=your_password
export PGPORT=5432

export MYSQL_HOST=localhost
export MYSQL_USER=root
export MYSQL_PASSWORD=your_password
export MYSQL_PORT=3306

export MONGO_URI=mongodb://user:pass@localhost:27017/dbname
```

## Features

- Create, edit, pause, resume, and delete backup jobs
- Run jobs manually or let scheduled jobs fire while the app is open
- Browse all backups across job types from one view
- Restore the latest backup with confirmation
- Clean up old backups by retention window

Supported schedule values:

| Value | Meaning |
|-------|---------|
| `1h` | every hour |
| `24h` | every day |
| `7d` | every week |
| `oneoff` | manual only |

Scheduling is in-app only for now. If `backup-xd` is closed, missed jobs do not run until you open it again.

## Controls

| Key | Action |
|-----|--------|
| `j/k`, `up/down` | Move |
| `enter` | Select |
| `a` | Add job |
| `e` | Edit selected job |
| `space` | Run selected job now |
| `p` | Pause or resume job |
| `ctrl+r` | Restore latest backup for selected job |
| `d`, `del` | Delete selected backup or job where supported |
| `?` | Help |
| `esc` | Back |
| `q` | Quit |

## License

[AGPL-3.0](LICENSE)
