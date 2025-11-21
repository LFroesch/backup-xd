package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type BackupJob struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Source      string    `json:"source"`
	Destination string    `json:"destination"`
	Type        string    `json:"type"`     // postgres, mysql, mongodb, file, directory
	Schedule    string    `json:"schedule"` // 1h, 24h, 7d, oneoff, etc.
	Status      string    `json:"status"`   // active, paused, running, error, completed
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
	Jobs     []BackupJob `json:"jobs"`
	NextID   int         `json:"next_id"`
	Settings Settings    `json:"settings"`
}

type Settings struct {
	BackupBasePath    string `json:"backup_base_path"`
	DefaultRetention  int    `json:"default_retention_days"`
	ColorTheme        string `json:"color_theme"` // "default" or "alternative"
	AutoRefresh       bool   `json:"auto_refresh"`
	VerifyBackups     bool   `json:"verify_backups"`
}

// Screen types for navigation
type screen int

const (
	screenMain screen = iota
	screenBackupManagement
	screenSettings
	screenGlobalBackups
	screenBackupView
	screenBackupClean
)

type model struct {
	jobs       []BackupJob
	table      table.Model
	config     BackupConfig
	configFile string

	// Navigation
	screen screen
	cursor int
	message string

	// Edit mode
	editMode  bool
	editRow   int
	editCol   int
	textInput textinput.Model

	// Delete confirmation mode
	deleteMode       bool
	deleteTargetIdx  int
	deleteTargetName string

	// Global backup viewing
	globalBackups     []BackupMetadata
	selectedBackup    *BackupMetadata
	
	// Global backup delete confirmation
	globalDeleteMode       bool
	globalDeleteTargetIdx  int
	globalDeleteTargetName string

	// Cleanup functionality
	cleanupMode       bool
	cleanupDays       int
	cleanupConfirm    bool
	cleanupPreview    []BackupMetadata

	// Search/filter
	searchMode   bool
	searchQuery  string
	filteredJobs []BackupJob

	// Settings edit mode
	settingsEditMode bool
	settingsEditRow  int
	settingsInput    textinput.Model

	width        int
	height       int
	statusMsg    string
	statusExpiry time.Time
	lastUpdate   time.Time
}

type statusMsg struct {
	message string
}

type tickMsg time.Time
type backupCompleteMsg struct {
	jobID   int
	success bool
	message string
}

func showStatus(msg string) tea.Cmd {
	return func() tea.Msg {
		return statusMsg{message: msg}
	}
}

// expandPath expands tilde (~) to the user's home directory
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		usr, err := user.Current()
		if err != nil {
			return path
		}
		return filepath.Join(usr.HomeDir, path[2:])
	}
	return path
}

func loadEnvironmentFile() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	envFile := filepath.Join(homeDir, ".config", "backup-xd", ".backup-env")
	data, err := os.ReadFile(envFile)
	if err != nil {
		return // File doesn't exist, that's OK
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		line = strings.TrimPrefix(line, "export ")

		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
			os.Setenv(key, value)
		}
	}
}

func loadBackupConfig(configFile string) BackupConfig {
	var config BackupConfig
	data, err := os.ReadFile(configFile)
	if err != nil {
		// Create default config with sample job using new backup structure
		backupBaseDir := getBackupBaseDir()
		config = BackupConfig{
			Jobs: []BackupJob{
				{
					ID:          1,
					Name:        "Database",
					Source:      "db or program name",
					Destination: filepath.Join(backupBaseDir, "backup-xd", "postgres"),
					Type:        "postgres",
					Schedule:    "24h",
					Status:      "active",
					LastRun:     time.Now().Add(-12 * time.Hour),
					NextRun:     time.Now().Add(12 * time.Hour),
					LastResult:  "Success",
					CreatedAt:   time.Now(),
				},
			},
			NextID: 2,
			Settings: Settings{
				BackupBasePath:   backupBaseDir,
				DefaultRetention: 30,
				ColorTheme:       "default",
				AutoRefresh:      true,
				VerifyBackups:    true,
			},
		}
		saveConfig(config, configFile)
		return config
	}

	json.Unmarshal(data, &config)

	// Ensure settings have defaults if not set
	if config.Settings.BackupBasePath == "" {
		config.Settings.BackupBasePath = getBackupBaseDir()
	}
	if config.Settings.DefaultRetention == 0 {
		config.Settings.DefaultRetention = 30
	}
	if config.Settings.ColorTheme == "" {
		config.Settings.ColorTheme = "default"
	}

	return config
}

func saveConfig(config BackupConfig, configFile string) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configFile, data, 0644)
}

func getBackupBaseDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Printf("Error getting home directory: %v", err)
		return "./backups" // fallback
	}
	return filepath.Join(homeDir, "backups")
}

func getBackupBaseDirFromSettings(settings Settings) string {
	if settings.BackupBasePath != "" {
		return expandPath(settings.BackupBasePath)
	}
	return getBackupBaseDir()
}

// Add this function to scan all global backups
func scanGlobalBackups() []BackupMetadata {
	return scanGlobalBackupsWithPath(getBackupBaseDir())
}

func scanGlobalBackupsWithPath(backupBaseDir string) []BackupMetadata {
	var allBackups []BackupMetadata

	// Scan all backup type directories
	backupTypes := []string{"postgres", "mysql", "mongodb", "files", "directories"}

	for _, backupType := range backupTypes {
		typeDir := filepath.Join(backupBaseDir, "backup-xd", backupType)

		entries, err := os.ReadDir(typeDir)
		if err != nil {
			continue // Directory doesn't exist, skip
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			// Look for metadata.json in each backup directory
			metadataPath := filepath.Join(typeDir, entry.Name(), "metadata.json")
			if metadata, err := loadBackupMetadata(metadataPath); err == nil {
				allBackups = append(allBackups, metadata)
			}
		}
	}

	// Sort by timestamp (newest first)
	sort.Slice(allBackups, func(i, j int) bool {
		return allBackups[i].Timestamp.After(allBackups[j].Timestamp)
	})

	return allBackups
}

// Add this function to load backup metadata
func loadBackupMetadata(metadataPath string) (BackupMetadata, error) {
	var metadata BackupMetadata
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return metadata, err
	}

	err = json.Unmarshal(data, &metadata)
	return metadata, err
}

// Add this function to get old backups for cleanup
func getOldBackups(days int) []BackupMetadata {
	allBackups := scanGlobalBackups()
	cutoffDate := time.Now().AddDate(0, 0, -days)

	var oldBackups []BackupMetadata
	for _, backup := range allBackups {
		if backup.Timestamp.Before(cutoffDate) {
			oldBackups = append(oldBackups, backup)
		}
	}

	return oldBackups
}

// NewModel creates a new model with initialized values
func NewModel() model {
	// Load environment variables from ~/.backup-env
	loadEnvironmentFile()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}

	configFile := filepath.Join(homeDir, ".config", "backup-xd", "config.json")
	config := loadBackupConfig(configFile)

	m := model{
		jobs:       config.Jobs,
		config:     config,
		configFile: configFile,
		screen:     screenMain,
		cursor:     0,
		width:      100,
		height:     24,
		editMode:   false,
		editRow:    -1,
		editCol:    -1,
		lastUpdate: time.Now(),
		globalBackups: scanGlobalBackups(),
		cleanupDays:   30, // Default cleanup period
	}

	// Initialize text input for editing
	m.textInput = textinput.New()
	m.textInput.CharLimit = 200

	// Initialize settings input
	m.settingsInput = textinput.New()
	m.settingsInput.CharLimit = 500

	// Initialize table
	columns := []table.Column{
		{Title: "ID", Width: 4},
		{Title: "Name", Width: 20},
		{Title: "Type", Width: 10},
		{Title: "Source", Width: 25},
		{Title: "Schedule", Width: 10},
		{Title: "Status", Width: 10},
		{Title: "Last Run", Width: 12},
		{Title: "Result", Width: 15},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		BorderBottom(true).
		Bold(true).
		Foreground(lipgloss.Color("#F3F4F6")).
		Background(lipgloss.Color("#1F2937"))
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("#F3F4F6")).
		Background(lipgloss.Color("#7C3AED")).
		Bold(true)
	s.Cell = s.Cell.
		Foreground(lipgloss.Color("#E5E7EB"))
	t.SetStyles(s)

	m.table = t
	m.updateTable()

	return m
}

func main() {
	// Check for daemon mode
	if len(os.Args) > 1 && os.Args[1] == "daemon" {
		runDaemon()
		return
	}

	m := NewModel()

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func runDaemon() {
	log.Println("Starting backup-xd daemon...")

	// Load environment variables
	loadEnvironmentFile()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}

	configFile := filepath.Join(homeDir, ".config", "backup-xd", "config.json")

	// Main daemon loop
	for {
		config := loadBackupConfig(configFile)

		now := time.Now()
		for i, job := range config.Jobs {
			// Skip paused, completed, or running jobs
			if job.Status == "paused" || job.Status == "completed" || job.Status == "running" {
				continue
			}

			// Skip oneoff jobs (they don't auto-run)
			if strings.ToLower(job.Schedule) == "oneoff" {
				continue
			}

			// Check if it's time to run this job
			shouldRun := false
			if job.LastRun.IsZero() {
				// Never run before, run now
				shouldRun = true
			} else {
				// Calculate next run based on schedule
				var nextRun time.Time
				schedule := strings.ToLower(job.Schedule)
				switch schedule {
				case "1h":
					nextRun = job.LastRun.Add(1 * time.Hour)
				case "24h":
					nextRun = job.LastRun.Add(24 * time.Hour)
				case "7d":
					nextRun = job.LastRun.Add(7 * 24 * time.Hour)
				default:
					log.Printf("Unknown schedule %s for job %s, skipping", job.Schedule, job.Name)
					continue
				}

				if now.After(nextRun) {
					shouldRun = true
				}
			}

			if shouldRun {
				log.Printf("Running scheduled backup: %s (ID: %d)", job.Name, job.ID)
				config.Jobs[i].Status = "running"
				saveConfig(config, configFile)

				// Run the backup
				startTime := time.Now()
				timestamp := startTime.Format("2006-01-02_15-04-05")
				var backupErr error
				var backupPath string

				switch job.Type {
				case "postgres":
					backupPath, backupErr = backupPostgresWithMetadata(job, timestamp, startTime)
				case "mysql":
					backupPath, backupErr = backupMySQLWithMetadata(job, timestamp, startTime)
				case "mongodb":
					connectionString := job.Source
					if strings.HasPrefix(connectionString, "$") {
						envVar := strings.TrimPrefix(connectionString, "$")
						connectionString = os.Getenv(envVar)
						if connectionString == "" {
							backupErr = fmt.Errorf("environment variable %s not set", envVar)
						} else {
							backupPath, backupErr = backupMongoDBWithMetadata(job, connectionString, timestamp, startTime)
						}
					} else {
						backupPath, backupErr = backupMongoDBWithMetadata(job, connectionString, timestamp, startTime)
					}
				case "file":
					backupPath, backupErr = backupFileWithMetadata(job, timestamp, startTime)
				case "directory":
					backupPath, backupErr = backupDirectoryWithMetadata(job, timestamp, startTime)
				default:
					backupErr = fmt.Errorf("unknown backup type: %s", job.Type)
				}

				// Update job status
				config.Jobs[i].LastRun = time.Now()
				if backupErr != nil {
					log.Printf("Backup failed for %s: %v", job.Name, backupErr)
					config.Jobs[i].Status = "error"
					config.Jobs[i].LastResult = "Error"
				} else {
					// Verify if enabled
					if config.Settings.VerifyBackups && backupPath != "" {
						var verifyPath string
						switch job.Type {
						case "postgres", "mysql":
							verifyPath = filepath.Join(backupPath, job.Source+".sql")
						case "directory":
							basename := filepath.Base(expandPath(job.Source))
							verifyPath = filepath.Join(backupPath, basename+".tar.gz")
						case "file":
							basename := filepath.Base(expandPath(job.Source))
							verifyPath = filepath.Join(backupPath, basename)
						case "mongodb":
							verifyPath = backupPath
						default:
							verifyPath = backupPath
						}

						if verifyErr := verifyBackup(job.Type, verifyPath); verifyErr != nil {
							log.Printf("Backup verification failed for %s: %v", job.Name, verifyErr)
							config.Jobs[i].Status = "error"
							config.Jobs[i].LastResult = "Verify Failed"
						} else {
							log.Printf("Backup completed and verified: %s", job.Name)
							config.Jobs[i].Status = "active"
							config.Jobs[i].LastResult = "Success (verified)"
						}
					} else {
						log.Printf("Backup completed: %s", job.Name)
						config.Jobs[i].Status = "active"
						config.Jobs[i].LastResult = "Success"
					}
				}

				saveConfig(config, configFile)
			}
		}

		// Sleep for 1 minute before checking again
		time.Sleep(1 * time.Minute)
	}
}

func (m *model) updateTable() {
	var rows []table.Row
	jobsToDisplay := m.jobs
	if m.filteredJobs != nil {
		jobsToDisplay = m.filteredJobs
	}

	for _, job := range jobsToDisplay {
		lastRun := "Never"
		if !job.LastRun.IsZero() {
			lastRun = job.LastRun.Format("01-02 15:04")
		}

		rows = append(rows, table.Row{
			strconv.Itoa(job.ID),
			job.Name,
			job.Type,
			job.Source,
			job.Schedule,
			job.Status,
			lastRun,
			job.LastResult,
		})
	}
	m.table.SetRows(rows)
}

func (m *model) adjustLayout() {
	tableHeight := m.height - 6
	if tableHeight < 5 {
		tableHeight = 5
	}

	// Smart column sizing
	availableWidth := m.width - 10
	idWidth := 4
	nameWidth := max(20, availableWidth/6)
	typeWidth := 10
	sourceWidth := max(25, availableWidth/3)
	scheduleWidth := 10
	statusWidth := 10
	lastRunWidth := 12
	resultWidth := max(15, availableWidth/6)

	columns := []table.Column{
		{Title: "ID", Width: idWidth},
		{Title: "Name", Width: nameWidth},
		{Title: "Type", Width: typeWidth},
		{Title: "Source", Width: sourceWidth},
		{Title: "Schedule", Width: scheduleWidth},
		{Title: "Status", Width: statusWidth},
		{Title: "Last Run", Width: lastRunWidth},
		{Title: "Result", Width: resultWidth},
	}

	m.table.SetColumns(columns)
	m.table.SetHeight(tableHeight)
}

func (m *model) startEdit() {
	if len(m.jobs) == 0 {
		return
	}

	m.editMode = true
	m.editRow = m.table.Cursor()
	m.editCol = 1 // Start with name column (skip ID)

	job := m.jobs[m.editRow]
	switch m.editCol {
	case 1:
		m.textInput.SetValue(job.Name)
	case 2:
		m.textInput.SetValue(job.Type)
	case 3:
		m.textInput.SetValue(job.Source)
	case 4:
		m.textInput.SetValue(job.Schedule)
	}
	m.textInput.Focus()
}

func (m *model) saveEdit() {
	if !m.editMode || m.editRow < 0 || m.editRow >= len(m.jobs) {
		return
	}

	value := m.textInput.Value()
	switch m.editCol {
	case 1:
		m.jobs[m.editRow].Name = value
	case 2:
		m.jobs[m.editRow].Type = value
	case 3:
		m.jobs[m.editRow].Source = value
	case 4:
		m.jobs[m.editRow].Schedule = value
	}

	m.config.Jobs = m.jobs
	saveConfig(m.config, m.configFile)
	m.updateTable()
}

func (m *model) cancelEdit() {
	m.editMode = false
	m.editRow = -1
	m.editCol = -1
	m.textInput.Blur()
	m.textInput.SetValue("")
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second*2, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tea.SetWindowTitle("backup-xd"),
		tickCmd(),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case statusMsg:
		m.statusMsg = msg.message
		m.statusExpiry = time.Now().Add(3 * time.Second)
		return m, nil

	case tickMsg:
		m.lastUpdate = time.Time(msg)
		return m, tickCmd()

	case backupCompleteMsg:
		for i, job := range m.jobs {
			if job.ID == msg.jobID {
				m.jobs[i].LastRun = time.Now()

				// Handle OneOff jobs - mark as completed
				if strings.ToLower(job.Schedule) == "oneoff" {
					m.jobs[i].Status = "completed"
				} else {
					m.jobs[i].Status = "active"
				}

				if msg.success {
					m.jobs[i].LastResult = "Success"
				} else {
					m.jobs[i].LastResult = "Error"
				}
				m.config.Jobs = m.jobs
				saveConfig(m.config, m.configFile)
				m.updateTable()
				break
			}
		}
		return m, showStatus(msg.message)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.adjustLayout()
		return m, nil

	case tea.KeyMsg:
		if m.editMode {
			return m.updateEdit(msg)
		}
		if m.deleteMode {
			return m.updateDelete(msg)
		}
		return m.updateNormal(msg)
	}

	if !m.editMode && !m.deleteMode {
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m model) updateDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		// Confirm deletion
		jobName := m.deleteTargetName
		m.jobs = append(m.jobs[:m.deleteTargetIdx], m.jobs[m.deleteTargetIdx+1:]...)
		m.config.Jobs = m.jobs
		saveConfig(m.config, m.configFile)
		m.updateTable()
		m.deleteMode = false
		m.deleteTargetIdx = -1
		m.deleteTargetName = ""
		return m, showStatus(fmt.Sprintf("🗑️ Deleted %s", jobName))
	case "n", "N", "esc":
		// Cancel deletion
		m.deleteMode = false
		m.deleteTargetIdx = -1
		m.deleteTargetName = ""
		return m, nil
	}
	return m, nil
}

func (m model) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searchMode = false
		m.searchQuery = ""
		m.filteredJobs = nil
		m.textInput.Blur()
		m.textInput.SetValue("")
		m.updateTable()
		return m, nil
	case "enter":
		m.searchMode = false
		m.searchQuery = m.textInput.Value()
		m.textInput.Blur()

		// Filter jobs based on search query
		if m.searchQuery == "" {
			m.filteredJobs = nil
		} else {
			query := strings.ToLower(m.searchQuery)
			m.filteredJobs = []BackupJob{}
			for _, job := range m.jobs {
				if strings.Contains(strings.ToLower(job.Name), query) ||
					strings.Contains(strings.ToLower(job.Type), query) ||
					strings.Contains(strings.ToLower(job.Status), query) ||
					strings.Contains(strings.ToLower(job.Source), query) {
					m.filteredJobs = append(m.filteredJobs, job)
				}
			}
		}
		m.updateTable()
		if len(m.filteredJobs) > 0 {
			return m, showStatus(fmt.Sprintf("🔍 Found %d matching jobs", len(m.filteredJobs)))
		} else if m.searchQuery != "" {
			return m, showStatus("🔍 No matching jobs found")
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m model) updateSettingsEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.settingsEditMode = false
		m.settingsEditRow = -1
		m.settingsInput.Blur()
		m.settingsInput.SetValue("")
		return m, nil
	case "enter":
		value := m.settingsInput.Value()
		switch m.settingsEditRow {
		case 0: // Backup Base Path
			m.config.Settings.BackupBasePath = value
		case 1: // Default Retention Days
			if days, err := strconv.Atoi(value); err == nil && days > 0 {
				m.config.Settings.DefaultRetention = days
			}
		case 2: // Color Theme
			if value == "default" || value == "alternative" {
				m.config.Settings.ColorTheme = value
			}
		case 3: // Auto Refresh
			m.config.Settings.AutoRefresh = value == "true" || value == "yes" || value == "1"
		case 4: // Verify Backups
			m.config.Settings.VerifyBackups = value == "true" || value == "yes" || value == "1"
		}

		saveConfig(m.config, m.configFile)
		m.settingsEditMode = false
		m.settingsEditRow = -1
		m.settingsInput.Blur()
		m.settingsInput.SetValue("")
		return m, showStatus("✅ Settings updated")
	case "tab", "down":
		m.settingsEditRow = (m.settingsEditRow + 1) % 5
		m.loadSettingsValue()
		return m, nil
	case "shift+tab", "up":
		m.settingsEditRow = (m.settingsEditRow - 1 + 5) % 5
		m.loadSettingsValue()
		return m, nil
	}

	var cmd tea.Cmd
	m.settingsInput, cmd = m.settingsInput.Update(msg)
	return m, cmd
}

func (m *model) loadSettingsValue() {
	switch m.settingsEditRow {
	case 0:
		m.settingsInput.SetValue(m.config.Settings.BackupBasePath)
	case 1:
		m.settingsInput.SetValue(strconv.Itoa(m.config.Settings.DefaultRetention))
	case 2:
		m.settingsInput.SetValue(m.config.Settings.ColorTheme)
	case 3:
		if m.config.Settings.AutoRefresh {
			m.settingsInput.SetValue("true")
		} else {
			m.settingsInput.SetValue("false")
		}
	case 4:
		if m.config.Settings.VerifyBackups {
			m.settingsInput.SetValue("true")
		} else {
			m.settingsInput.SetValue("false")
		}
	}
}

func (m model) updateEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.cancelEdit()
		return m, nil
	case "enter":
		m.saveEdit()
		m.cancelEdit()
		return m, showStatus("✅ Job updated")
	case "tab":
		m.saveEdit()
		m.editCol++
		if m.editCol > 4 {
			m.editCol = 1
		}
		job := m.jobs[m.editRow]
		switch m.editCol {
		case 1:
			m.textInput.SetValue(job.Name)
		case 2:
			m.textInput.SetValue(job.Type)
		case 3:
			m.textInput.SetValue(job.Source)
		case 4:
			m.textInput.SetValue(job.Schedule)
		}
		return m, nil
	case "shift+tab":
		m.saveEdit()
		m.editCol--
		if m.editCol < 1 {
			m.editCol = 4
		}
		job := m.jobs[m.editRow]
		switch m.editCol {
		case 1:
			m.textInput.SetValue(job.Name)
		case 2:
			m.textInput.SetValue(job.Type)
		case 3:
			m.textInput.SetValue(job.Source)
		case 4:
			m.textInput.SetValue(job.Schedule)
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle search mode
	if m.searchMode {
		return m.updateSearch(msg)
	}

	// Handle settings edit mode
	if m.settingsEditMode {
		return m.updateSettingsEdit(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.globalDeleteMode {
			return m, nil // Disable navigation during delete confirmation
		}
		if m.screen == screenBackupManagement {
			// Let the table handle navigation for backup management
			var cmd tea.Cmd
			m.table, cmd = m.table.Update(msg)
			return m, cmd
		} else {
			// Handle menu navigation for other screens
			if m.cursor > 0 {
				m.cursor--
			}
		}
		return m, nil
	case "down", "j":
		if m.globalDeleteMode {
			return m, nil // Disable navigation during delete confirmation
		}
		if m.screen == screenBackupManagement {
			// Let the table handle navigation for backup management
			var cmd tea.Cmd
			m.table, cmd = m.table.Update(msg)
			return m, cmd
		} else {
			// Handle menu navigation for other screens
			maxCursor := m.getMaxCursor()
			if m.cursor < maxCursor {
				m.cursor++
			}
		}
		return m, nil
	case "enter":
		if m.screen == screenBackupManagement {
			// In backup management, enter runs the selected backup
			if len(m.jobs) > 0 {
				job := m.jobs[m.table.Cursor()]
				return m, m.runBackup(job.ID)
			}
			return m, nil
		} else {
			// In other screens, enter handles menu navigation
			return m.handleEnter()
		}
	case "esc":
		if m.globalDeleteMode {
			m.globalDeleteMode = false
			m.globalDeleteTargetIdx = -1
			m.globalDeleteTargetName = ""
			return m, nil
		}
		// Clear search filter when going back
		if m.screen == screenBackupManagement && m.filteredJobs != nil {
			m.filteredJobs = nil
			m.searchQuery = ""
			m.updateTable()
			return m, showStatus("🔍 Filter cleared")
		}
		if m.screen != screenMain {
			m.screen = screenMain
			m.cursor = 0
			m.message = ""
			m.cleanupConfirm = false
			return m, nil
		}
		return m, tea.Quit
	case "v":
		if m.screen == screenGlobalBackups && len(m.globalBackups) > 0 && !m.globalDeleteMode {
			return m.handleViewBackupDetails()
		}
	case "d":
		if m.screen == screenGlobalBackups && len(m.globalBackups) > 0 && !m.globalDeleteMode {
			return m.handleDeleteGlobalBackup()
		}
	case "c":
		if m.screen == screenBackupClean && len(m.cleanupPreview) > 0 && !m.cleanupConfirm {
			m.cleanupConfirm = true
			return m, nil
		}
	case "y":
		if m.screen == screenBackupClean && m.cleanupConfirm {
			return m.performCleanup()
		}
		if m.screen == screenGlobalBackups && m.globalDeleteMode {
			return m.performGlobalBackupDelete()
		}
	case "n":
		if m.screen == screenBackupClean && m.cleanupConfirm {
			m.cleanupConfirm = false
			return m, nil
		}
		if m.screen == screenGlobalBackups && m.globalDeleteMode {
			m.globalDeleteMode = false
			m.globalDeleteTargetIdx = -1
			m.globalDeleteTargetName = ""
			return m, nil
		}
	case "+", "=":
		if m.screen == screenBackupClean && !m.cleanupConfirm {
			if m.cleanupDays < 365 {
				m.cleanupDays++
				m.cleanupPreview = getOldBackups(m.cleanupDays)
				m.message = fmt.Sprintf("Cleanup period set to %d days", m.cleanupDays)
			}
		}
	case "-", "_":
		if m.screen == screenBackupClean && !m.cleanupConfirm {
			if m.cleanupDays > 1 {
				m.cleanupDays--
				m.cleanupPreview = getOldBackups(m.cleanupDays)
				m.message = fmt.Sprintf("Cleanup period set to %d days", m.cleanupDays)
			}
		}

	// Keep the old backup management functionality for when in the backup management screen
	case "e":
		if m.screen == screenBackupManagement {
			m.startEdit()
		} else if m.screen == screenSettings {
			m.settingsEditMode = true
			m.settingsEditRow = m.cursor
			m.loadSettingsValue()
			m.settingsInput.Focus()
		}
		return m, nil
	case "a":
		if m.screen == screenBackupManagement {
			newJob := BackupJob{
				ID:          m.config.NextID,
				Name:        "New Backup Job",
				Source:      "/path/to/source",
				Destination: "./backups/",
				Type:        "file",
				Schedule:    "oneoff",
				Status:      "active",
				LastResult:  "Not run",
				CreatedAt:   time.Now(),
			}
			m.config.NextID++
			m.jobs = append(m.jobs, newJob)
			m.config.Jobs = m.jobs
			saveConfig(m.config, m.configFile)
			m.updateTable()
			m.table.SetCursor(len(m.jobs) - 1)
			m.startEdit()
			return m, showStatus("➕ New backup job added")
		}
	case "delete":
		if m.screen == screenBackupManagement && len(m.jobs) > 0 {
			idx := m.table.Cursor()
			m.deleteMode = true
			m.deleteTargetIdx = idx
			m.deleteTargetName = m.jobs[idx].Name
		}
		return m, nil
	case " ":
		if m.screen == screenBackupManagement && len(m.jobs) > 0 {
			job := m.jobs[m.table.Cursor()]
			return m, m.runBackup(job.ID)
		}
		return m, nil
	case "ctrl+r":
		if m.screen == screenBackupManagement && len(m.jobs) > 0 {
			job := m.jobs[m.table.Cursor()]
			return m, m.showRestoreOptions(job)
		}
		return m, nil
	case "r":
		if m.screen == screenBackupManagement {
			m.config = loadBackupConfig(m.configFile)
			m.jobs = m.config.Jobs
			m.updateTable()
			return m, showStatus("🔄 Refreshed")
		}
	case "/":
		if m.screen == screenBackupManagement {
			m.searchMode = true
			m.searchQuery = ""
			m.textInput.SetValue("")
			m.textInput.Focus()
			return m, nil
		}
	case "p":
		if m.screen == screenBackupManagement && len(m.jobs) > 0 {
			idx := m.table.Cursor()
			currentStatus := m.jobs[idx].Status

			// Don't allow pausing/resuming completed oneoff jobs
			if currentStatus == "completed" && strings.ToLower(m.jobs[idx].Schedule) == "oneoff" {
				return m, showStatus("❌ Cannot resume completed OneOff job")
			}

			if currentStatus == "active" {
				m.jobs[idx].Status = "paused"
			} else if currentStatus == "paused" {
				m.jobs[idx].Status = "active"
			} else if currentStatus == "completed" {
				m.jobs[idx].Status = "active"
			}

			m.config.Jobs = m.jobs
			saveConfig(m.config, m.configFile)
			m.updateTable()
			return m, showStatus(fmt.Sprintf("⏸️ Job %s", m.jobs[idx].Status))
		}
		return m, nil
	default:
		if m.screen == screenBackupManagement {
			var cmd tea.Cmd
			m.table, cmd = m.table.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

func (m model) runBackup(jobID int) tea.Cmd {
	return func() tea.Msg {
		for i, job := range m.jobs {
			if job.ID == jobID {
				m.jobs[i].Status = "running"

				startTime := time.Now()
				timestamp := startTime.Format("2006-01-02_15-04-05")
				var err error
				var backupPath string

				switch job.Type {
				case "postgres":
					backupPath, err = backupPostgresWithMetadata(job, timestamp, startTime)
				case "mysql":
					backupPath, err = backupMySQLWithMetadata(job, timestamp, startTime)
				case "mongodb":
					connectionString := job.Source
					if strings.HasPrefix(connectionString, "$") {
						envVar := strings.TrimPrefix(connectionString, "$")
						connectionString = os.Getenv(envVar)
						if connectionString == "" {
							err = fmt.Errorf("environment variable %s not set", envVar)
							break
						}
					}
					backupPath, err = backupMongoDBWithMetadata(job, connectionString, timestamp, startTime)
				case "file":
					backupPath, err = backupFileWithMetadata(job, timestamp, startTime)
				case "directory":
					backupPath, err = backupDirectoryWithMetadata(job, timestamp, startTime)
				default:
					err = fmt.Errorf("unknown backup type: %s", job.Type)
				}

				if err != nil {
					return backupCompleteMsg{
						jobID:   jobID,
						success: false,
						message: fmt.Sprintf("❌ Backup failed: %v", err),
					}
				}

				// Verify backup if enabled in settings
				cfg := loadBackupConfig(m.configFile)
				verifyEnabled := cfg.Settings.VerifyBackups

				if verifyEnabled && backupPath != "" {
					// Find the actual backup file/directory to verify
					var verifyPath string
					switch job.Type {
					case "postgres", "mysql":
						// For SQL backups, path is to directory, need to find .sql file
						verifyPath = filepath.Join(backupPath, job.Source+".sql")
					case "directory":
						// For directories, find the .tar.gz file
						basename := filepath.Base(expandPath(job.Source))
						verifyPath = filepath.Join(backupPath, basename+".tar.gz")
					case "file":
						// For files, find the original filename
						basename := filepath.Base(expandPath(job.Source))
						verifyPath = filepath.Join(backupPath, basename)
					case "mongodb":
						// For MongoDB, verify the directory itself
						verifyPath = backupPath
					default:
						verifyPath = backupPath
					}

					verifyErr := verifyBackup(job.Type, verifyPath)
					if verifyErr != nil {
						return backupCompleteMsg{
							jobID:   jobID,
							success: false,
							message: fmt.Sprintf("❌ Backup verification failed: %v", verifyErr),
						}
					}
				}

				successMsg := fmt.Sprintf("✅ Backup completed: %s", job.Name)
				if verifyEnabled {
					successMsg += " (verified)"
				}
				if strings.ToLower(job.Schedule) == "oneoff" {
					successMsg += " (OneOff job marked as completed)"
				}

				return backupCompleteMsg{
					jobID:   jobID,
					success: true,
					message: successMsg,
				}
			}
		}

		return backupCompleteMsg{
			jobID:   jobID,
			success: false,
			message: "❌ Job not found",
		}
	}
}

func (m model) showRestoreOptions(job BackupJob) tea.Cmd {
	return func() tea.Msg {
		backupFiles, err := findBackupsForJob(job)
		if err != nil {
			return statusMsg{message: fmt.Sprintf("❌ Error finding backups: %v", err)}
		}

		if len(backupFiles) == 0 {
			return statusMsg{message: "❌ No backups found for this job"}
		}

		latestBackup := backupFiles[len(backupFiles)-1]

		connectionString := job.Source
		if strings.HasPrefix(connectionString, "$") {
			envVar := strings.TrimPrefix(connectionString, "$")
			connectionString = os.Getenv(envVar)
			if connectionString == "" {
				return statusMsg{message: fmt.Sprintf("❌ Environment variable %s not set", envVar)}
			}
		}

		var err2 error
		switch job.Type {
		case "postgres":
			err2 = restorePostgres(job.Source, latestBackup)
		case "mongodb":
			err2 = restoreMongoDB(connectionString, latestBackup)
		case "file":
			err2 = restoreFile(expandPath(job.Source), latestBackup)
		case "directory":
			err2 = restoreDirectory(expandPath(job.Source), latestBackup)
		default:
			err2 = fmt.Errorf("restore not supported for type: %s", job.Type)
		}

		if err2 != nil {
			return statusMsg{message: fmt.Sprintf("❌ Restore failed: %v", err2)}
		}

		return statusMsg{message: fmt.Sprintf("✅ Restored: %s from %s", job.Name, filepath.Base(latestBackup))}
	}
}

func findBackupsForJob(job BackupJob) ([]string, error) {
	var backupFiles []string

	// Use the backup base directory structure
	backupBaseDir := getBackupBaseDir()
	var expandedDestination string

	if strings.HasPrefix(job.Destination, backupBaseDir) {
		expandedDestination = job.Destination
	} else {
		// Map to new structure based on backup type
		switch job.Type {
		case "postgres":
			expandedDestination = filepath.Join(backupBaseDir, "backup-xd", "postgres")
		case "mysql":
			expandedDestination = filepath.Join(backupBaseDir, "backup-xd", "mysql")
		case "mongodb":
			expandedDestination = filepath.Join(backupBaseDir, "backup-xd", "mongodb")
		case "file":
			expandedDestination = filepath.Join(backupBaseDir, "backup-xd", "files")
		case "directory":
			expandedDestination = filepath.Join(backupBaseDir, "backup-xd", "directories")
		default:
			expandedDestination = expandPath(job.Destination)
		}
	}

	switch job.Type {
	case "postgres", "mysql":
		// Look for backup directories with pattern {source}_{timestamp}
		entries, err := os.ReadDir(expandedDestination)
		if err != nil {
			return nil, err
		}

		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), strings.ReplaceAll(job.Name, " ", "_")+"_") {
				// Check if the SQL file exists inside the directory
				sqlFile := filepath.Join(expandedDestination, entry.Name(), job.Source+".sql")
				if _, err := os.Stat(sqlFile); err == nil {
					backupFiles = append(backupFiles, sqlFile)
				}
			}
		}

	case "mongodb":
		entries, err := os.ReadDir(expandedDestination)
		if err != nil {
			return nil, err
		}

		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), strings.ReplaceAll(job.Name, " ", "_")+"_") {
				backupFiles = append(backupFiles, filepath.Join(expandedDestination, entry.Name()))
			}
		}

	case "file":
		// Look for backup directories with pattern {jobName}_{timestamp}
		entries, err := os.ReadDir(expandedDestination)
		if err != nil {
			return nil, err
		}

		expandedSource := expandPath(job.Source)
		baseName := filepath.Base(expandedSource)
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), strings.ReplaceAll(job.Name, " ", "_")+"_") {
				// Check if the file exists inside the directory
				backupFile := filepath.Join(expandedDestination, entry.Name(), baseName)
				if _, err := os.Stat(backupFile); err == nil {
					backupFiles = append(backupFiles, backupFile)
				}
			}
		}

	case "directory":
		// Look for backup directories with pattern {jobName}_{timestamp}
		entries, err := os.ReadDir(expandedDestination)
		if err != nil {
			return nil, err
		}

		expandedSource := expandPath(job.Source)
		baseName := filepath.Base(expandedSource)
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), strings.ReplaceAll(job.Name, " ", "_")+"_") {
				// Check if the tar.gz file exists inside the directory
				tarFile := filepath.Join(expandedDestination, entry.Name(), baseName+".tar.gz")
				if _, err := os.Stat(tarFile); err == nil {
					backupFiles = append(backupFiles, tarFile)
				}
			}
		}
	}

	return backupFiles, nil
}

// Add new menu option in renderMain
func (m model) renderMain() string {
	options := []string{
		"📋 Backup Management",
		"💾 View All Backups (Global)",
		"🧹 Cleanup Old Backups",
		"⚙️  Settings",
		"❌ Quit",
	}

	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("99")).
		Render("backup-xd - Backup Management")

	selectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("57")).
		Foreground(lipgloss.Color("230"))

	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	var rows []string
	for i, option := range options {
		if i == m.cursor {
			rows = append(rows, selectedStyle.Render("> "+option))
		} else {
			rows = append(rows, normalStyle.Render("  "+option))
		}
	}

	instructions := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Render("\nPress Enter to select, ↑↓ to navigate, q to quit")

	messageStr := ""
	if m.message != "" {
		messageStr = "\n\n" + lipgloss.NewStyle().
			Foreground(lipgloss.Color("226")).
			Render(m.message)
	}

	return fmt.Sprintf("%s\n\n%s%s%s", header, strings.Join(rows, "\n"), instructions, messageStr)
}

// Add rendering function for global backups
func (m model) renderGlobalBackups() string {
	if len(m.globalBackups) == 0 {
		return "No backups found in global backup directory.\n\nPress ESC to go back"
	}

	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("99")).
		Render(fmt.Sprintf("💾 Global Backups (%d total)", len(m.globalBackups)))

	selectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("57")).
		Foreground(lipgloss.Color("230"))

	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	var rows []string
	for i, backup := range m.globalBackups {
		// Format: JobName | Type | Size | Age | Status
		age := formatAge(backup.Timestamp)
		row := fmt.Sprintf("%-20s %-10s %-10s %-10s %s",
			truncate(backup.JobName, 20),
			backup.BackupType,
			backup.FileSizeStr,
			age,
			backup.Status)

		if i == m.cursor {
			row = selectedStyle.Render("> " + row)
		} else {
			row = normalStyle.Render("  " + row)
		}

		rows = append(rows, row)
	}

	var instructions string
	if m.globalDeleteMode {
		deleteStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#DC2626")).
			Bold(true)

		instructions = deleteStyle.Render(fmt.Sprintf("\n🗑️  Delete backup '%s'? ", m.globalDeleteTargetName)) +
			lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("y: yes • n/esc: no")
	} else {
		instructions = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render("\nPress 'v' to view details, 'd' to delete, ESC to go back")
	}

	messageStr := ""
	if m.message != "" {
		messageStr = "\n\n" + lipgloss.NewStyle().
			Foreground(lipgloss.Color("226")).
			Render(m.message)
	}

	return fmt.Sprintf("%s\n\n%s%s%s", header, strings.Join(rows, "\n"), instructions, messageStr)
}

// Add rendering function for backup cleanup
func (m model) renderBackupClean() string {
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("99")).
		Render("🧹 Cleanup Old Backups")

	settingsText := fmt.Sprintf("Cleanup backups older than: %d days\n\n", m.cleanupDays)

	if len(m.cleanupPreview) > 0 {
		settingsText += fmt.Sprintf("Found %d backups to delete:\n", len(m.cleanupPreview))

		var totalSize int64
		for _, backup := range m.cleanupPreview {
			settingsText += fmt.Sprintf("  • %s (%s) - %s\n",
				backup.JobName,
				backup.FileSizeStr,
				formatAge(backup.Timestamp))
			totalSize += backup.FileSize
		}

		settingsText += fmt.Sprintf("\nTotal space to reclaim: %s\n", formatFileSize(totalSize))

		if m.cleanupConfirm {
			settingsText += "\n⚠️  Are you sure you want to delete these backups? This cannot be undone!"
		}
	} else {
		settingsText += "No backups older than this period found."
	}

	var instructions string
	if m.cleanupConfirm {
		instructions = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render("\nPress 'y' to confirm deletion, 'n' or ESC to cancel")
	} else {
		instructions = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render("\nPress +/- to adjust days, 'c' to clean, ESC to go back")
	}

	messageStr := ""
	if m.message != "" {
		messageStr = "\n\n" + lipgloss.NewStyle().
			Foreground(lipgloss.Color("226")).
			Render(m.message)
	}

	return fmt.Sprintf("%s\n\n%s%s%s", header, settingsText, instructions, messageStr)
}

func (m model) renderSettings() string {
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("99")).
		Render("⚙️  Settings")

	settings := []struct {
		name  string
		value string
		hint  string
	}{
		{"Backup Base Path", m.config.Settings.BackupBasePath, "Path where backups are stored"},
		{"Default Retention Days", strconv.Itoa(m.config.Settings.DefaultRetention), "Default days to keep backups"},
		{"Color Theme", m.config.Settings.ColorTheme, "UI color theme (default/alternative)"},
		{"Auto Refresh", fmt.Sprintf("%t", m.config.Settings.AutoRefresh), "Automatically refresh job list (true/false)"},
		{"Verify Backups", fmt.Sprintf("%t", m.config.Settings.VerifyBackups), "Verify backup integrity after creation (true/false)"},
	}

	selectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("57")).
		Foreground(lipgloss.Color("230"))

	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	var rows []string
	for i, setting := range settings {
		line := fmt.Sprintf("%-25s: %s", setting.name, setting.value)
		hint := hintStyle.Render(fmt.Sprintf("  (%s)", setting.hint))

		if m.settingsEditMode && i == m.settingsEditRow {
			editStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#F59E0B")).
				Bold(true)
			line = editStyle.Render(fmt.Sprintf("✏️  %-25s: %s", setting.name, m.settingsInput.View()))
			rows = append(rows, line)
		} else if i == m.cursor && !m.settingsEditMode {
			rows = append(rows, selectedStyle.Render("> "+line)+hint)
		} else {
			rows = append(rows, normalStyle.Render("  "+line)+hint)
		}
	}

	var instructions string
	if m.settingsEditMode {
		instructions = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render("\nPress Enter to save • Tab/↑↓ to switch fields • ESC to cancel")
	} else {
		instructions = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render("\nPress 'e' to edit • ↑↓ to navigate • ESC to go back")
	}

	messageStr := ""
	if m.message != "" {
		messageStr = "\n\n" + lipgloss.NewStyle().
			Foreground(lipgloss.Color("226")).
			Render(m.message)
	}

	return fmt.Sprintf("%s\n\n%s%s%s", header, strings.Join(rows, "\n"), instructions, messageStr)
}

// Update handleEnter to handle new menu options
func (m model) handleEnter() (model, tea.Cmd) {
	switch m.screen {
	case screenMain:
		switch m.cursor {
		case 0: // Backup Management
			m.screen = screenBackupManagement
		case 1: // View All Backups (Global)
			m.screen = screenGlobalBackups
			m.globalBackups = scanGlobalBackups() // Refresh the list
		case 2: // Cleanup Old Backups
			m.screen = screenBackupClean
			m.cleanupPreview = getOldBackups(m.cleanupDays)
		case 3: // Settings
			m.screen = screenSettings
		case 4: // Quit
			return m, tea.Quit
		}
		m.cursor = 0
		m.message = ""
	}

	return m, nil
}

// Update getMaxCursor to handle new screens
func (m model) getMaxCursor() int {
	switch m.screen {
	case screenMain:
		return 4 // Updated to account for new menu options
	case screenSettings:
		return 4 // 5 settings items (0-4)
	case screenGlobalBackups:
		return len(m.globalBackups) - 1
	case screenBackupClean:
		return 0 // No cursor navigation needed
	default:
		return 0
	}
}

// Add handler functions
func (m model) handleViewBackupDetails() (model, tea.Cmd) {
	if m.cursor < len(m.globalBackups) {
		backup := m.globalBackups[m.cursor]
		m.selectedBackup = &backup
		m.screen = screenBackupView
		m.cursor = 0
	}
	return m, nil
}

func (m model) handleDeleteGlobalBackup() (model, tea.Cmd) {
	if m.cursor < len(m.globalBackups) {
		backup := m.globalBackups[m.cursor]
		m.globalDeleteMode = true
		m.globalDeleteTargetIdx = m.cursor
		m.globalDeleteTargetName = backup.JobName
	}
	return m, nil
}

func (m model) performGlobalBackupDelete() (model, tea.Cmd) {
	if m.globalDeleteTargetIdx >= 0 && m.globalDeleteTargetIdx < len(m.globalBackups) {
		backup := m.globalBackups[m.globalDeleteTargetIdx]
		backupBaseDir := getBackupBaseDir()
		
		// Map job types to directory names
		dirName := backup.BackupType
		switch backup.BackupType {
		case "file":
			dirName = "files"
		case "directory":
			dirName = "directories"
		}
		
		backupPath := filepath.Join(backupBaseDir, "backup-xd", dirName, backup.BackupFile)
		
		// Try to delete the backup
		if err := os.RemoveAll(backupPath); err != nil {
			m.message = fmt.Sprintf("❌ Failed to delete backup: %v", err)
		} else {
			m.message = fmt.Sprintf("✅ Deleted backup: %s", backup.JobName)
			// Remove from the slice
			m.globalBackups = append(m.globalBackups[:m.globalDeleteTargetIdx], m.globalBackups[m.globalDeleteTargetIdx+1:]...)
			// Adjust cursor if necessary
			if m.cursor >= len(m.globalBackups) && len(m.globalBackups) > 0 {
				m.cursor = len(m.globalBackups) - 1
			} else if len(m.globalBackups) == 0 {
				m.cursor = 0
			}
		}
	}
	
	// Reset delete mode
	m.globalDeleteMode = false
	m.globalDeleteTargetIdx = -1
	m.globalDeleteTargetName = ""
	
	return m, nil
}

func (m model) performCleanup() (model, tea.Cmd) {
	var deletedCount int
	var errors []string
	var reclaimedSize int64

	backupBaseDir := getBackupBaseDir()

	for _, backup := range m.cleanupPreview {
		// Construct the full path to the backup directory
		backupPath := filepath.Join(backupBaseDir, "backup-xd", backup.BackupType, backup.BackupFile)

		if err := os.RemoveAll(backupPath); err != nil {
			errors = append(errors, fmt.Sprintf("Failed to delete %s: %v", backup.JobName, err))
		} else {
			deletedCount++
			reclaimedSize += backup.FileSize
		}
	}

	// Refresh the global backups list
	m.globalBackups = scanGlobalBackups()
	m.cleanupPreview = getOldBackups(m.cleanupDays)
	m.cleanupConfirm = false

	if len(errors) > 0 {
		m.message = fmt.Sprintf("✅ Deleted %d backups (%s reclaimed). ❌ Errors: %s",
			deletedCount, formatFileSize(reclaimedSize), strings.Join(errors, "; "))
	} else {
		m.message = fmt.Sprintf("✅ Cleaned up %d backups, reclaimed %s",
			deletedCount, formatFileSize(reclaimedSize))
	}

	return m, nil
}

func (m model) View() string {
	// Handle different screens
	switch m.screen {
	case screenMain:
		return m.renderMain()
	case screenBackupManagement:
		return m.ViewOld() // Use the original backup management view
	case screenSettings:
		return m.renderSettings()
	case screenGlobalBackups:
		return m.renderGlobalBackups()
	case screenBackupClean:
		return m.renderBackupClean()
	default:
		return "Screen not implemented yet"
	}
}

func (m model) ViewOld() string {
	// Define styles
	errorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#EF4444")).
		Bold(true)

	successStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#10B981")).
		Bold(true)

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280"))

	if len(m.jobs) == 0 {
		emptyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).
			MarginTop(1).
			MarginBottom(1)

		content := emptyStyle.Render("📋 No backup jobs configured yet.\n\n💡 Press 'n' to add your first backup job!")
		footer := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#60A5FA")).
			Render("Commands: ") +
			lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF24")).Render("n/a: add job") +
			lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render(" • ") +
			lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171")).Render("q: quit")

		return lipgloss.JoinVertical(lipgloss.Left,
			content,
			footer,
		)
	}

	// Job count info
	activeJobs := 0
	completedJobs := 0
	pausedJobs := 0
	for _, job := range m.jobs {
		switch job.Status {
		case "completed":
			completedJobs++
		case "active":
			activeJobs++
		case "paused":
			pausedJobs++
		}
	}

	// Title
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#7C3AED")).
		Bold(true)
	title := titleStyle.Render("backup-xd")

	statsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#9CA3AF")).
		Background(lipgloss.Color("#111827")).
		Padding(0, 1).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151"))
	statsText := statsStyle.Render(fmt.Sprintf("📊 Jobs: %d total | %d active | %d paused | %d completed",
		len(m.jobs), activeJobs, pausedJobs, completedJobs))

	// Combine title and stats on same line
	stats := lipgloss.JoinHorizontal(lipgloss.Center, title, "  ", statsText)

	// Status message with proper styling
	var statusMessage string
	if m.statusMsg != "" && time.Now().Before(m.statusExpiry) {
		if strings.Contains(m.statusMsg, "❌") || strings.Contains(m.statusMsg, "Failed") {
			statusMessage = errorStyle.Render("Status: " + m.statusMsg)
		} else {
			statusMessage = successStyle.Render("Status: " + m.statusMsg)
		}
	}

	// Footer with commands
	var footer string
	if m.searchMode {
		searchStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3B82F6")).
			Bold(true)

		footer = searchStyle.Render("🔍 Search: "+m.textInput.View()) +
			helpStyle.Render("\nPress Enter to search • ESC to cancel")
	} else if m.deleteMode {
		deleteStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#DC2626")).
			Bold(true)

		footer = deleteStyle.Render(fmt.Sprintf("🗑️  Delete '%s'? ", m.deleteTargetName)) +
			helpStyle.Render("y: yes • n/esc: no")
	} else if m.editMode {
		colNames := []string{"", "Name", "Type", "Source", "Schedule"}
		colName := colNames[m.editCol]
		editStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F59E0B")).
			Bold(true)

		scheduleHelp := ""
		if colName == "Schedule" {
			scheduleHelp = " (e.g. 1h, 24h, 7d, oneoff)"
		}

		footer = editStyle.Render(fmt.Sprintf("✏️  Editing %s%s: %s", colName, scheduleHelp, m.textInput.View())) +
			helpStyle.Render("\nCommands: tab: next field • enter: save • esc: cancel")
	} else {
		// Style individual command groups with colors
		navStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#60A5FA"))    // Blue
		actionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#34D399")) // Green
		editStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF24"))   // Yellow
		systemStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171")) // Red

		commandsHelp := []string{
			navStyle.Render("↑↓: navigate"),
			actionStyle.Render("space/enter: backup"),
			actionStyle.Render("ctrl+r: restore"),
			editStyle.Render("\ne: edit"),
			editStyle.Render("n/a: add"),
			editStyle.Render("p: pause/resume"),
			systemStyle.Render("/: search"),
			systemStyle.Render("d: delete"),
			systemStyle.Render("r: refresh"),
			systemStyle.Render("q: quit"),
		}
		footer = helpStyle.Render("Commands: " + strings.Join(commandsHelp[:3], " • ") + " • " + strings.Join(commandsHelp[3:], " • "))

		// Add search filter indicator if active
		if m.filteredJobs != nil && m.searchQuery != "" {
			filterStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#3B82F6")).
				Background(lipgloss.Color("#1E3A8A")).
				Padding(0, 1)
			footer = filterStyle.Render(fmt.Sprintf("🔍 Filtered by: '%s' (%d results)", m.searchQuery, len(m.filteredJobs))) + "\n" + footer
		}
	}

	// Build the final view
	var parts []string

	// Always include stats
	parts = append(parts, stats)

	// Add table
	parts = append(parts, m.table.View())

	// Add status message if present
	if statusMessage != "" {
		parts = append(parts, statusMessage)
	}

	// Add footer
	parts = append(parts, footer)

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// Backup functions with metadata
func backupPostgresWithMetadata(job BackupJob, timestamp string, startTime time.Time) (string, error) {
	// Use the backup base directory structure
	backupBaseDir := getBackupBaseDir()
	backupDir := filepath.Join(backupBaseDir, "backup-xd", "postgres")

	// Expand the destination path if it's not already using the new structure
	var expandedDestination string
	if strings.HasPrefix(job.Destination, backupBaseDir) {
		expandedDestination = job.Destination
	} else {
		expandedDestination = backupDir
	}

	folderName := fmt.Sprintf("%s_%s", strings.ReplaceAll(job.Name, " ", "_"), timestamp)
	folderPath := filepath.Join(expandedDestination, folderName)
	filename := fmt.Sprintf("%s.sql", job.Source)
	fullPath := filepath.Join(folderPath, filename)

	os.MkdirAll(folderPath, 0755)

	err := backupPostgres(job.Source, folderPath, timestamp)
	if err != nil {
		return "", err
	}

	endTime := time.Now()
	duration := endTime.Sub(startTime)

	fileSize, _ := getFileSize(fullPath)

	metadata := BackupMetadata{
		JobID:       job.ID,
		JobName:     job.Name,
		BackupType:  job.Type,
		Source:      job.Source,
		BackupFile:  folderName,
		Timestamp:   startTime,
		HumanTime:   startTime.Format("January 2, 2006 at 3:04 PM"),
		FileSize:    fileSize,
		FileSizeStr: formatFileSize(fileSize),
		Duration:    duration.String(),
		Status:      "completed",
	}

	saveBackupMetadata(metadata, filepath.Join(folderPath, "metadata.json"))
	return folderPath, nil
}

func backupPostgres(dbName, destination, timestamp string) error {
	filename := fmt.Sprintf("%s.sql", dbName)
	fullPath := filepath.Join(destination, filename)

	os.MkdirAll(destination, 0755)

	pgHost := os.Getenv("PGHOST")
	pgUser := os.Getenv("PGUSER")
	pgPassword := os.Getenv("PGPASSWORD")
	pgPort := os.Getenv("PGPORT")

	if pgHost == "" {
		pgHost = "localhost"
	}
	if pgUser == "" {
		pgUser = "postgres"
	}
	if pgPassword == "" {
		pgPassword = "postgres"
	}
	if pgPort == "" {
		pgPort = "5432"
	}

	cmd := exec.Command("pg_dump",
		"-h", pgHost,
		"-U", pgUser,
		"-p", pgPort,
		"-d", dbName,
		"-f", fullPath)

	cmd.Env = append(os.Environ(), "PGPASSWORD="+pgPassword)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}

	return cmd.Run()
}

func backupMySQLWithMetadata(job BackupJob, timestamp string, startTime time.Time) (string, error) {
	backupBaseDir := getBackupBaseDir()
	backupDir := filepath.Join(backupBaseDir, "backup-xd", "mysql")

	var expandedDestination string
	if strings.HasPrefix(job.Destination, backupBaseDir) {
		expandedDestination = job.Destination
	} else {
		expandedDestination = backupDir
	}

	folderName := fmt.Sprintf("%s_%s", strings.ReplaceAll(job.Name, " ", "_"), timestamp)
	folderPath := filepath.Join(expandedDestination, folderName)
	filename := fmt.Sprintf("%s.sql", job.Source)
	fullPath := filepath.Join(folderPath, filename)

	os.MkdirAll(folderPath, 0755)

	err := backupMySQL(job.Source, folderPath, timestamp)
	if err != nil {
		return "", err
	}

	endTime := time.Now()
	duration := endTime.Sub(startTime)

	fileSize, _ := getFileSize(fullPath)

	metadata := BackupMetadata{
		JobID:       job.ID,
		JobName:     job.Name,
		BackupType:  job.Type,
		Source:      job.Source,
		BackupFile:  folderName,
		Timestamp:   startTime,
		HumanTime:   startTime.Format("January 2, 2006 at 3:04 PM"),
		FileSize:    fileSize,
		FileSizeStr: formatFileSize(fileSize),
		Duration:    duration.String(),
		Status:      "completed",
	}

	saveBackupMetadata(metadata, filepath.Join(folderPath, "metadata.json"))
	return folderPath, nil
}

func backupMySQL(dbName, destination, timestamp string) error {
	filename := fmt.Sprintf("%s.sql", dbName)
	fullPath := filepath.Join(destination, filename)

	os.MkdirAll(destination, 0755)

	// Get MySQL credentials from environment
	mysqlUser := os.Getenv("MYSQL_USER")
	mysqlPassword := os.Getenv("MYSQL_PWD")
	mysqlHost := os.Getenv("MYSQL_HOST")
	mysqlPort := os.Getenv("MYSQL_PORT")

	if mysqlUser == "" {
		mysqlUser = "root"
	}
	if mysqlHost == "" {
		mysqlHost = "localhost"
	}
	if mysqlPort == "" {
		mysqlPort = "3306"
	}

	args := []string{
		"-h", mysqlHost,
		"-P", mysqlPort,
		"-u", mysqlUser,
	}

	// Only add password flag if password is set
	if mysqlPassword != "" {
		args = append(args, fmt.Sprintf("-p%s", mysqlPassword))
	}

	args = append(args, dbName)

	cmd := exec.Command("mysqldump", args...)

	file, err := os.Create(fullPath)
	if err != nil {
		return err
	}
	defer file.Close()

	cmd.Stdout = file
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}

	return cmd.Run()
}

func backupMongoDBWithMetadata(job BackupJob, connectionString, timestamp string, startTime time.Time) (string, error) {
	backupBaseDir := getBackupBaseDir()
	backupDir := filepath.Join(backupBaseDir, "backup-xd", "mongodb")

	var expandedDestination string
	if strings.HasPrefix(job.Destination, backupBaseDir) {
		expandedDestination = job.Destination
	} else {
		expandedDestination = backupDir
	}

	dirname := fmt.Sprintf("%s_%s", strings.ReplaceAll(job.Name, " ", "_"), timestamp)
	dirpath := filepath.Join(expandedDestination, dirname)

	err := backupMongoDB(connectionString, expandedDestination, timestamp, job.Name)
	if err != nil {
		return "", err
	}

	endTime := time.Now()
	duration := endTime.Sub(startTime)

	fileSize, _ := getFileSize(dirpath)

	metadata := BackupMetadata{
		JobID:       job.ID,
		JobName:     job.Name,
		BackupType:  job.Type,
		Source:      job.Source,
		BackupFile:  dirname,
		Timestamp:   startTime,
		HumanTime:   startTime.Format("January 2, 2006 at 3:04 PM"),
		FileSize:    fileSize,
		FileSizeStr: formatFileSize(fileSize),
		Duration:    duration.String(),
		Status:      "completed",
	}

	saveBackupMetadata(metadata, filepath.Join(dirpath, "metadata.json"))
	return dirpath, nil
}

func backupMongoDB(connectionString, destination, timestamp, jobName string) error {
	dirname := fmt.Sprintf("%s_%s", strings.ReplaceAll(jobName, " ", "_"), timestamp)
	dirpath := filepath.Join(destination, dirname)

	os.MkdirAll(destination, 0755)

	cmd := exec.Command("mongodump", "--uri", connectionString, "--out", dirpath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mongodump failed: %v\nOutput: %s", err, string(output))
	}

	return nil
}

func backupFileWithMetadata(job BackupJob, timestamp string, startTime time.Time) (string, error) {
	backupBaseDir := getBackupBaseDir()
	backupDir := filepath.Join(backupBaseDir, "backup-xd", "files")

	var expandedDestination string
	if strings.HasPrefix(job.Destination, backupBaseDir) {
		expandedDestination = job.Destination
	} else {
		expandedDestination = backupDir
	}

	expandedSource := expandPath(job.Source)
	folderName := fmt.Sprintf("%s_%s", strings.ReplaceAll(job.Name, " ", "_"), timestamp)
	folderPath := filepath.Join(expandedDestination, folderName)

	os.MkdirAll(folderPath, 0755)

	err := backupFile(expandedSource, folderPath, timestamp)
	if err != nil {
		return "", err
	}

	endTime := time.Now()
	duration := endTime.Sub(startTime)

	fileSize, _ := getFileSize(folderPath)

	metadata := BackupMetadata{
		JobID:       job.ID,
		JobName:     job.Name,
		BackupType:  job.Type,
		Source:      job.Source,
		BackupFile:  folderName,
		Timestamp:   startTime,
		HumanTime:   startTime.Format("January 2, 2006 at 3:04 PM"),
		FileSize:    fileSize,
		FileSizeStr: formatFileSize(fileSize),
		Duration:    duration.String(),
		Status:      "completed",
	}

	saveBackupMetadata(metadata, filepath.Join(folderPath, "metadata.json"))
	return folderPath, nil
}

func backupFile(source, destination, timestamp string) error {
	basename := filepath.Base(source)
	fullPath := filepath.Join(destination, basename)

	os.MkdirAll(destination, 0755)

	cmd := exec.Command("cp", source, fullPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}

	return cmd.Run()
}

func backupDirectoryWithMetadata(job BackupJob, timestamp string, startTime time.Time) (string, error) {
	backupBaseDir := getBackupBaseDir()
	backupDir := filepath.Join(backupBaseDir, "backup-xd", "directories")

	var expandedDestination string
	if strings.HasPrefix(job.Destination, backupBaseDir) {
		expandedDestination = job.Destination
	} else {
		expandedDestination = backupDir
	}

	expandedSource := expandPath(job.Source)
	folderName := fmt.Sprintf("%s_%s", strings.ReplaceAll(job.Name, " ", "_"), timestamp)
	folderPath := filepath.Join(expandedDestination, folderName)

	os.MkdirAll(folderPath, 0755)

	err := backupDirectory(expandedSource, folderPath, timestamp)
	if err != nil {
		return "", err
	}

	endTime := time.Now()
	duration := endTime.Sub(startTime)

	fileSize, _ := getFileSize(folderPath)

	metadata := BackupMetadata{
		JobID:       job.ID,
		JobName:     job.Name,
		BackupType:  job.Type,
		Source:      job.Source,
		BackupFile:  folderName,
		Timestamp:   startTime,
		HumanTime:   startTime.Format("January 2, 2006 at 3:04 PM"),
		FileSize:    fileSize,
		FileSizeStr: formatFileSize(fileSize),
		Duration:    duration.String(),
		Status:      "completed",
	}

	saveBackupMetadata(metadata, filepath.Join(folderPath, "metadata.json"))
	return folderPath, nil
}

func backupDirectory(source, destination, timestamp string) error {
	basename := filepath.Base(source)
	filename := fmt.Sprintf("%s.tar.gz", basename)
	fullPath := filepath.Join(destination, filename)

	os.MkdirAll(destination, 0755)

	cmd := exec.Command("tar", "-czf", fullPath, "-C", filepath.Dir(source), filepath.Base(source))
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}

	return cmd.Run()
}

// Restore functions
func restorePostgres(dbName, backupFile string) error {
	pgHost := os.Getenv("PGHOST")
	pgUser := os.Getenv("PGUSER")
	pgPassword := os.Getenv("PGPASSWORD")
	pgPort := os.Getenv("PGPORT")

	if pgHost == "" {
		pgHost = "localhost"
	}
	if pgUser == "" {
		pgUser = "postgres"
	}
	if pgPassword == "" {
		pgPassword = "postgres"
	}
	if pgPort == "" {
		pgPort = "5432"
	}

	cmd := exec.Command("psql",
		"-h", pgHost,
		"-U", pgUser,
		"-p", pgPort,
		"-d", dbName,
		"-f", backupFile)

	cmd.Env = append(os.Environ(), "PGPASSWORD="+pgPassword)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}

	return cmd.Run()
}

func restoreMongoDB(connectionString, backupDir string) error {
	cmd := exec.Command("mongorestore",
		"--uri", connectionString,
		"--drop",
		backupDir)

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mongorestore failed: %v\nOutput: %s", err, string(output))
	}

	return nil
}

func restoreFile(originalPath, backupFile string) error {
	cmd := exec.Command("cp", backupFile, originalPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}

	return cmd.Run()
}

func restoreDirectory(originalPath, backupFile string) error {
	cmd := exec.Command("tar", "-xzf", backupFile, "-C", filepath.Dir(originalPath))
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}

	return cmd.Run()
}

func formatFileSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func getFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}

	if info.IsDir() {
		var size int64
		err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				size += info.Size()
			}
			return nil
		})
		return size, err
	}

	return info.Size(), nil
}

func saveBackupMetadata(metadata BackupMetadata, metadataFile string) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metadataFile, data, 0644)
}

// Add helper functions
func formatAge(timestamp time.Time) string {
	age := time.Since(timestamp)

	if age < time.Hour {
		return fmt.Sprintf("%dm", int(age.Minutes()))
	} else if age < 24*time.Hour {
		return fmt.Sprintf("%dh", int(age.Hours()))
	} else {
		return fmt.Sprintf("%dd", int(age.Hours()/24))
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// verifyBackup performs basic integrity checks on backups
func verifyBackup(backupType, path string) error {
	switch backupType {
	case "postgres", "mysql":
		// For SQL backups, check if file exists and contains valid SQL
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("cannot read backup file: %v", err)
		}
		if len(data) == 0 {
			return fmt.Errorf("backup file is empty")
		}
		// Basic SQL validation - check for common SQL keywords
		content := string(data)
		if !strings.Contains(content, "CREATE") && !strings.Contains(content, "INSERT") && !strings.Contains(content, "TABLE") {
			return fmt.Errorf("backup file does not appear to contain valid SQL")
		}
		return nil

	case "mongodb":
		// For MongoDB, check if directory exists and contains BSON files
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("cannot access backup directory: %v", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("backup path is not a directory")
		}
		// Check for BSON files
		entries, err := os.ReadDir(path)
		if err != nil {
			return fmt.Errorf("cannot read backup directory: %v", err)
		}
		hasBSON := false
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".bson") || strings.HasSuffix(entry.Name(), ".metadata.json") {
				hasBSON = true
				break
			}
		}
		if !hasBSON {
			return fmt.Errorf("backup directory does not contain BSON files")
		}
		return nil

	case "directory":
		// For tar.gz, verify archive integrity
		cmd := exec.Command("tar", "-tzf", path)
		cmd.Stdout = nil
		cmd.Stderr = nil
		err := cmd.Run()
		if err != nil {
			return fmt.Errorf("archive integrity check failed: %v", err)
		}
		return nil

	case "file":
		// For file backups, just check if file exists and is not empty
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("cannot access backup file: %v", err)
		}
		if info.Size() == 0 {
			return fmt.Errorf("backup file is empty")
		}
		return nil

	default:
		// Unknown type, skip verification
		return nil
	}
}
