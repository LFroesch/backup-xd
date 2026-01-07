# backup-xd

A terminal-based backup management system built with Go and Bubble Tea. Create, schedule, and manage backups for databases, files, and directories with an intuitive TUI interface.

## Features

- **Database Backups**: PostgreSQL, MySQL, MongoDB support
- **File System Backups**: Individual files and directories  
- **Backup Management**: View, edit, pause/resume, and delete backup jobs
- **Global Backup View**: Browse all backups across different types
- **Automated Cleanup**: Remove old backups based on retention policies
- **Restore Operations**: Quick restore from latest backups
- **Metadata Tracking**: Detailed backup information and statistics

## Installation

```bash
go install github.com/LFroesch/backup-xd@latest
```

Make sure `$GOPATH/bin` (usually `~/go/bin`) is in your PATH:
```bash
export PATH="$HOME/go/bin:$PATH"
```

## Configuration

The application stores configuration in `~/.config/backup-xd/`:
- `config.json` - Backup job definitions
- `.backup-env` - Environment variables for database connections

### Environment Variables

Set database connection details in `~/.config/backup-xd/.backup-env`:

```bash
# PostgreSQL
export PGHOST=localhost
export PGUSER=postgres  
export PGPASSWORD=your_password
export PGPORT=5432

# MongoDB connection strings can be stored as environment variables
export MONGO_URI=mongodb://user:pass@localhost:27017/dbname
```

## Usage

Run `backup-xd` to open the interactive interface:

- **=� Backup Management**: Create, edit, and run backup jobs
- **=� View All Backups**: Browse global backup history  
- **>� Cleanup**: Remove old backups to free space
- **�Settings**: Configure application preferences

### Backup Types

- `postgres` - PostgreSQL database dumps
- `mysql` - MySQL database dumps  
- `mongodb` - MongoDB collections
- `file` - Individual file backups
- `directory` - Compressed directory archives

### Schedule Options

- `1h`, `24h`, `7d` - Recurring intervals
- `oneoff` - Single execution (marked completed after run)

## Backup Storage

Backups are organized in `~/backups/backup-xd/` by type:
```
~/backups/backup-xd/
postgres/
mysql/
mongodb/
files/
directories/
```

Each backup includes metadata.json with timestamp, size, and job details.