package main

import (
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// --- Types ---

type BackupJob struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Source      string    `json:"source"`
	Destination string    `json:"destination"`
	Type        string    `json:"type"`
	Schedule    string    `json:"schedule"`
	Status      string    `json:"status"`
	LastRun     time.Time `json:"last_run"`
	NextRun     time.Time `json:"next_run"`
	LastResult  string    `json:"last_result"`
	CreatedAt   time.Time `json:"created_at"`
}

type BackupMetadata struct {
	JobID       int       `json:"job_id"`
	JobName     string    `json:"job_name"`
	BackupType  string    `json:"backup_type"`
	Source      string    `json:"source"`
	BackupFile  string    `json:"backup_file"`
	Timestamp   time.Time `json:"timestamp"`
	HumanTime   string    `json:"human_time"`
	FileSize    int64     `json:"file_size_bytes"`
	FileSizeStr string    `json:"file_size_human"`
	Duration    string    `json:"duration"`
	Status      string    `json:"status"`
	Error       string    `json:"error,omitempty"`
}

type BackupConfig struct {
	Jobs   []BackupJob `json:"jobs"`
	NextID int         `json:"next_id"`
}

type screen int

const (
	screenMain screen = iota
	screenBackupManagement
	screenGlobalBackups
	screenBackupView
	screenBackupClean
)

type model struct {
	jobs       []BackupJob
	table      table.Model
	config     BackupConfig
	configFile string

	screen  screen
	cursor  int
	message string

	editMode  bool
	editRow   int
	editCol   int
	textInput textinput.Model

	deleteMode       bool
	deleteTargetIdx  int
	deleteTargetName string

	restoreConfirm    bool
	restoreTargetIdx  int
	restoreTargetName string

	globalBackups  []BackupMetadata
	selectedBackup *BackupMetadata

	globalDeleteMode       bool
	globalDeleteTargetIdx  int
	globalDeleteTargetName string

	cleanupMode    bool
	cleanupDays    int
	cleanupConfirm bool
	cleanupPreview []BackupMetadata

	width        int
	height       int
	statusMsg    string
	statusExpiry time.Time
	lastUpdate   time.Time
	showHelp     bool
}

// --- Messages ---

type statusMsg struct {
	message string
}

type tickMsg time.Time
type backupCompleteMsg struct {
	jobID       int
	success     bool
	message     string
	completedAt time.Time
}

func showStatus(msg string) tea.Cmd {
	return func() tea.Msg {
		return statusMsg{message: msg}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second*2, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// --- Init ---

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tea.SetWindowTitle("backup-xd"),
		tickCmd(),
	)
}
