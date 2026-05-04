## DevLog
### 2026-05-04: One-off jobs made reusable and restore now confirms
Changed `oneoff` semantics from "run once ever" to "manual-only, runnable multiple times" by keeping one-off jobs active after completion and normalizing legacy completed one-offs back to active on load. Added a `ctrl+r` confirmation prompt before restore execution so latest-backup restores are no longer immediate. Updated README behavior docs and noted an older-backup restore picker as backlog in WORK.md. Files: model.go, helpers.go, update.go, view.go, README.md, WORK.md.

### 2026-05-04: V1 blocker pass completed
Implemented the global backup detail screen so the existing `v` keypath now renders real metadata instead of a placeholder, and added MySQL restore support so `ctrl+r` works across the advertised database backup types. Updated README keybindings/feature notes and cleared the stale v1 blocker list from WORK.md. Files: view.go, update.go, helpers.go, README.md, WORK.md.

### 2026-05-04: WORK audit refreshed against codebase
Checked the current v1 task list against the live code. `WORK.md` had gone stale: the remaining confirmed blockers are the unimplemented global backup detail screen (`screenBackupView` routes to a placeholder) and missing MySQL restore handling in the restore flow. Also noted that `go test ./...` still passes. Files: WORK.md.

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
