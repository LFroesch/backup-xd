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
- **Search & Filter**: Quickly find backup jobs by name, type, or status
- **Backup Verification**: Automatic integrity checks after backup creation
- **Daemon Mode**: Background scheduler for automatic backup execution
- **Configurable Settings**: Customize paths, retention, themes, and more

## Installation

```bash
make install
```

This builds the binary and copies it to `~/.local/bin/backup-xd`.

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

# MySQL
export MYSQL_HOST=localhost
export MYSQL_USER=root
export MYSQL_PWD=your_password
export MYSQL_PORT=3306

# MongoDB connection strings can be stored as environment variables
export MONGO_URI=mongodb://user:pass@localhost:27017/dbname
```

## Usage

### Interactive Mode

Run `backup-xd` to open the interactive interface:

- **📋 Backup Management**: Create, edit, and run backup jobs
- **💾 View All Backups**: Browse global backup history
- **🧹 Cleanup**: Remove old backups to free space
- **⚙️ Settings**: Configure application preferences

#### Keyboard Shortcuts (Backup Management)

- `↑↓/j/k` - Navigate jobs
- `Space/Enter` - Run backup
- `Ctrl+R` - Restore from latest backup
- `/` - Search/filter jobs
- `e` - Edit selected job
- `a/n` - Add new job
- `p` - Pause/resume job
- `Delete` - Delete job
- `r` - Refresh list
- `ESC` - Clear filter or go back
- `q` - Quit

### Daemon Mode

Run backups automatically in the background based on schedules:

```bash
backup-xd daemon
```

The daemon will:
- Check for scheduled jobs every minute
- Execute jobs based on their schedule (`1h`, `24h`, `7d`)
- Skip `oneoff` jobs (manual only)
- Skip `paused` or `completed` jobs
- Verify backups if enabled in settings
- Log all operations to stdout

**Tip**: Run with systemd, screen, or tmux for persistent operation.

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

Backups are organized in `~/backups/backup-xd/` by type (configurable via Settings):
```
~/backups/backup-xd/
├── postgres/
├── mysql/
├── mongodb/
├── files/
└── directories/
```

Each backup includes metadata.json with timestamp, size, and job details.

## Settings

Customize behavior via the Settings screen (`⚙️ Settings` in main menu):

- **Backup Base Path**: Where backups are stored (default: `~/backups`)
- **Default Retention Days**: Default cleanup period (default: 30 days)
- **Color Theme**: UI color scheme (default/alternative)
- **Auto Refresh**: Automatically refresh job list (true/false)
- **Verify Backups**: Run integrity checks after backup creation (true/false)

Press `e` to edit settings, navigate with `↑↓`, and press `Enter` to save.

## Backup Verification

When enabled in settings, backups are automatically verified:

- **PostgreSQL/MySQL**: Checks for valid SQL content
- **MongoDB**: Verifies BSON files exist
- **Directories**: Tests tar.gz archive integrity
- **Files**: Ensures file exists and is not empty

Failed verifications will mark the job as error status.