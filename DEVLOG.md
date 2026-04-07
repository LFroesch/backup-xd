## DevLog
### 2026-03-23: Doc suite added
Added CLAUDE.md, agent_spec.md. Updated README to scout standard. Documented all known issues.

### 2026-03-20: Full code audit
Audited all 1965 lines of main.go. Found: race condition (model mutation inside Cmd goroutine), two unimplemented screens (settings, backup detail view), MySQL hardcoded creds, dead `n` keybind, redundant `max()` function. Architecture is functional — supports postgres/mysql/mongodb/file/directory backups with metadata tracking, restore, cleanup, and global backup view. Needs file split and bug fixes before interview-ready.
