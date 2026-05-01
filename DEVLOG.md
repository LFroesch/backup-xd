## DevLog
### 2026-04-30: `n` keybind wired for adding jobs
Footer/help advertised `n/a: add` but only `a` worked. Extended the existing `n` handler in update.go to also create a new backup job when on screenBackupManagement, matching the behavior of `a`. Files: update.go.

### 2026-04-18: In-app scheduler enabled
Added an open-app scheduler driven by the Bubble Tea tick loop. Active jobs with due `next_run` values now start automatically while `backup-xd` is running, `running` status prevents duplicate launches, and recurring jobs now advance `last_run`/`next_run` after completion.

### 2026-04-18: Backup layout cleanup and docs correction
Removed the dead Settings menu entry so the main menu only exposes implemented screens. Normalized backup storage to `~/backups/backup-xd/<type>/<job-key>/<timestamp>/...` and updated scan/delete/restore discovery to match. Corrected README to document that `backup-xd` is currently TUI-only and that schedule values are metadata, not an in-app automatic scheduler.

### 2026-03-23: Doc suite added
Added CLAUDE.md, agent_spec.md. Updated README to scout standard. Documented all known issues.

### 2026-03-20: Full code audit
Audited all 1965 lines of main.go. Found: race condition (model mutation inside Cmd goroutine), two unimplemented screens (settings, backup detail view), MySQL hardcoded creds, dead `n` keybind, redundant `max()` function. Architecture is functional — supports postgres/mysql/mongodb/file/directory backups with metadata tracking, restore, cleanup, and global backup view. Needs file split and bug fixes before interview-ready.
