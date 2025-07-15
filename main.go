package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
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

type model struct {
	jobs       []BackupJob
	table      table.Model
	config     BackupConfig
	configFile string

	// Edit mode
	editMode  bool
	editRow   int
	editCol   int
	textInput textinput.Model

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

	envFile := filepath.Join(homeDir, ".backup-env")
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
		// Create default config with sample job
		config = BackupConfig{
			Jobs: []BackupJob{
				{
					ID:          1,
					Name:        "Database",
					Source:      "db or program name",
					Destination: "./backups/postgres/",
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
		}
		saveConfig(config, configFile)
		return config
	}

	json.Unmarshal(data, &config)
	return config
}

func saveConfig(config BackupConfig, configFile string) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configFile, data, 0644)
}

func main() {
	// Load environment variables from ~/.backup-env
	loadEnvironmentFile()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}

	configFile := filepath.Join(homeDir, ".local/bin/backup-xd-config.json")
	config := loadBackupConfig(configFile)

	m := model{
		jobs:       config.Jobs,
		config:     config,
		configFile: configFile,
		width:      100,
		height:     24,
		editMode:   false,
		editRow:    -1,
		editCol:    -1,
		lastUpdate: time.Now(),
	}

	// Initialize text input for editing
	m.textInput = textinput.New()
	m.textInput.CharLimit = 200

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

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func (m *model) updateTable() {
	var rows []table.Row
	for _, job := range m.jobs {
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
		return m.updateNormal(msg)
	}

	if !m.editMode {
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}

	return m, nil
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
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "e":
		m.startEdit()
		return m, nil
	case "n", "a":
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
	case "d", "delete":
		if len(m.jobs) > 0 {
			idx := m.table.Cursor()
			jobName := m.jobs[idx].Name
			m.jobs = append(m.jobs[:idx], m.jobs[idx+1:]...)
			m.config.Jobs = m.jobs
			saveConfig(m.config, m.configFile)
			m.updateTable()
			return m, showStatus(fmt.Sprintf("🗑️ Deleted %s", jobName))
		}
		return m, nil
	case " ", "enter":
		if len(m.jobs) > 0 {
			job := m.jobs[m.table.Cursor()]
			return m, m.runBackup(job.ID)
		}
		return m, nil
	case "ctrl+r":
		if len(m.jobs) > 0 {
			job := m.jobs[m.table.Cursor()]
			return m, m.showRestoreOptions(job)
		}
		return m, nil
	case "r":
		m.config = loadBackupConfig(m.configFile)
		m.jobs = m.config.Jobs
		m.updateTable()
		return m, showStatus("🔄 Refreshed")
	case "p":
		if len(m.jobs) > 0 {
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
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
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

				switch job.Type {
				case "postgres":
					_, err = backupPostgresWithMetadata(job, timestamp, startTime)
				case "mysql":
					_, err = backupMySQLWithMetadata(job, timestamp, startTime)
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
					_, err = backupMongoDBWithMetadata(job, connectionString, timestamp, startTime)
				case "file":
					_, err = backupFileWithMetadata(job, timestamp, startTime)
				case "directory":
					_, err = backupDirectoryWithMetadata(job, timestamp, startTime)
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

				successMsg := fmt.Sprintf("✅ Backup completed: %s", job.Name)
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
	expandedDestination := expandPath(job.Destination)

	switch job.Type {
	case "postgres", "mysql":
		// Look for backup directories with pattern {source}_{timestamp}
		entries, err := os.ReadDir(expandedDestination)
		if err != nil {
			return nil, err
		}

		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), job.Source+"_") {
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
			if entry.IsDir() && strings.Contains(entry.Name(), "_") {
				backupFiles = append(backupFiles, filepath.Join(expandedDestination, entry.Name()))
			}
		}

	case "file":
		// Look for backup directories with pattern {basename}_{timestamp}
		entries, err := os.ReadDir(expandedDestination)
		if err != nil {
			return nil, err
		}

		expandedSource := expandPath(job.Source)
		baseName := filepath.Base(expandedSource)
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), baseName+"_") {
				// Check if the file exists inside the directory
				backupFile := filepath.Join(expandedDestination, entry.Name(), baseName)
				if _, err := os.Stat(backupFile); err == nil {
					backupFiles = append(backupFiles, backupFile)
				}
			}
		}

	case "directory":
		// Look for backup directories with pattern {basename}_{timestamp}
		entries, err := os.ReadDir(expandedDestination)
		if err != nil {
			return nil, err
		}

		expandedSource := expandPath(job.Source)
		baseName := filepath.Base(expandedSource)
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), baseName+"_") {
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

func (m model) View() string {
	// Define styles
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F3F4F6")).
		Background(lipgloss.Color("#7C3AED")).
		Padding(0, 2).
		MarginBottom(1).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#A855F7"))

	errorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#EF4444")).
		Bold(true)

	successStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#10B981")).
		Bold(true)

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		MarginTop(1)

	// Header with app info
	header := titleStyle.Render("backup-xd")
	subtitle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#9CA3AF")).
		Render("Database & File Backup Scheduler")

	if len(m.jobs) == 0 {
		emptyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).
			MarginTop(2).
			MarginBottom(2)

		content := emptyStyle.Render("📋 No backup jobs configured yet.\n\n💡 Press 'n' to add your first backup job!")
		footer := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#60A5FA")).
			Render("Commands: ") +
			lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF24")).Render("n/a: add job") +
			lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280")).Render(" • ") +
			lipgloss.NewStyle().Foreground(lipgloss.Color("#F87171")).Render("q: quit")

		return lipgloss.JoinVertical(lipgloss.Left,
			"",
			header,
			subtitle,
			content,
			footer,
		)
	}

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
	if m.editMode {
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
			editStyle.Render("e: edit"),
			editStyle.Render("n/a: add"),
			editStyle.Render("p: pause/resume"),
			systemStyle.Render("d: delete"),
			systemStyle.Render("r: refresh"),
			systemStyle.Render("q: quit"),
		}
		footer = helpStyle.Render("Commands: " + strings.Join(commandsHelp, " • "))

		if statusMessage != "" {
			footer = statusMessage + "\n" + footer
		}
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

	statsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#9CA3AF")).
		Background(lipgloss.Color("#111827")).
		Padding(0, 1).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151"))
	stats := statsStyle.Render(fmt.Sprintf("📊 Jobs: %d total | %d active | %d paused | %d completed",
		len(m.jobs), activeJobs, pausedJobs, completedJobs))

	return lipgloss.JoinVertical(lipgloss.Left,
		"",
		header,
		subtitle,
		"",
		stats,
		"",
		m.table.View(),
		"",
		footer,
	)
}

// Backup functions with metadata
func backupPostgresWithMetadata(job BackupJob, timestamp string, startTime time.Time) (string, error) {
	expandedDestination := expandPath(job.Destination)
	
	folderName := fmt.Sprintf("%s_%s", job.Source, timestamp)
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
	expandedDestination := expandPath(job.Destination)
	
	folderName := fmt.Sprintf("%s_%s", job.Source, timestamp)
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

	cmd := exec.Command("mysqldump", "-u", "root", "-p", dbName)

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
	expandedDestination := expandPath(job.Destination)
	
	dbName := "database"
	if strings.Contains(connectionString, "/") {
		parts := strings.Split(connectionString, "/")
		if len(parts) > 1 {
			dbPart := parts[len(parts)-1]
			if strings.Contains(dbPart, "?") {
				dbName = strings.Split(dbPart, "?")[0]
			} else {
				dbName = dbPart
			}
		}
	}

	dirname := fmt.Sprintf("%s_%s", dbName, timestamp)
	dirpath := filepath.Join(expandedDestination, dirname)

	err := backupMongoDB(connectionString, expandedDestination, timestamp)
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
		FileSize:    fileSize,
		FileSizeStr: formatFileSize(fileSize),
		Duration:    duration.String(),
		Status:      "completed",
	}

	saveBackupMetadata(metadata, filepath.Join(dirpath, "metadata.json"))
	return dirpath, nil
}

func backupMongoDB(connectionString, destination, timestamp string) error {
	dbName := "database"
	if strings.Contains(connectionString, "/") {
		parts := strings.Split(connectionString, "/")
		if len(parts) > 1 {
			dbPart := parts[len(parts)-1]
			if strings.Contains(dbPart, "?") {
				dbName = strings.Split(dbPart, "?")[0]
			} else {
				dbName = dbPart
			}
		}
	}

	dirname := fmt.Sprintf("%s_%s", dbName, timestamp)
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
	expandedSource := expandPath(job.Source)
	expandedDestination := expandPath(job.Destination)
	
	basename := filepath.Base(expandedSource)
	folderName := fmt.Sprintf("%s_%s", basename, timestamp)
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
	expandedSource := expandPath(job.Source)
	expandedDestination := expandPath(job.Destination)
	
	basename := filepath.Base(expandedSource)
	folderName := fmt.Sprintf("%s_%s", basename, timestamp)
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
