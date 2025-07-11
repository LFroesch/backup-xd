package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
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
	Schedule    string    `json:"schedule"` // 1h, 24h, 7d, etc.
	Status      string    `json:"status"`   // active, paused, running, error
	LastRun     time.Time `json:"last_run"`
	NextRun     time.Time `json:"next_run"`
	LastResult  string    `json:"last_result"`
	CreatedAt   time.Time `json:"created_at"`
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

		if strings.HasPrefix(line, "export ") {
			line = strings.TrimPrefix(line, "export ")
		}

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
		// Create default config with sample Gator job
		config = BackupConfig{
			Jobs: []BackupJob{
				{
					ID:          1,
					Name:        "Gator Database",
					Source:      "gator",
					Destination: "./backups/postgres/",
					Type:        "postgres",
					Schedule:    "24h",
					Status:      "active",
					LastRun:     time.Now().Add(-12 * time.Hour),
					NextRun:     time.Now().Add(12 * time.Hour),
					LastResult:  "Success",
					CreatedAt:   time.Now(),
				},
				{
					ID:          2,
					Name:        "Project Manager DB",
					Source:      "$PROJECT_MANAGER_MONGODB_URI",
					Destination: "./backups/mongodb/",
					Type:        "mongodb",
					Schedule:    "24h",
					Status:      "active",
					LastRun:     time.Now().Add(-2 * 24 * time.Hour),
					NextRun:     time.Now().Add(5 * 24 * time.Hour),
					LastResult:  "Success",
					CreatedAt:   time.Now(),
				},
			},
			NextID: 3,
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

	configFile := filepath.Join(homeDir, ".local/bin/backup-manager-config.json")
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
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
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
		tea.SetWindowTitle("Backup Manager"),
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
				m.jobs[i].Status = "active"
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
			Schedule:    "24h",
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
			if m.jobs[idx].Status == "active" {
				m.jobs[idx].Status = "paused"
			} else if m.jobs[idx].Status == "paused" {
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

				timestamp := time.Now().Format("20060102_150405")
				var err error

				switch job.Type {
				case "postgres":
					err = backupPostgres(job.Source, job.Destination, timestamp)
				case "mysql":
					err = backupMySQL(job.Source, job.Destination, timestamp)
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
					err = backupMongoDB(connectionString, job.Destination, timestamp)
				case "file":
					err = backupFile(job.Source, job.Destination, timestamp)
				case "directory":
					err = backupDirectory(job.Source, job.Destination, timestamp)
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

				return backupCompleteMsg{
					jobID:   jobID,
					success: true,
					message: fmt.Sprintf("✅ Backup completed: %s", job.Name),
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
			err2 = restoreFile(job.Source, latestBackup)
		case "directory":
			err2 = restoreDirectory(job.Source, latestBackup)
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

	switch job.Type {
	case "postgres", "mysql":
		pattern := filepath.Join(job.Destination, "*.sql")
		files, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		backupFiles = files

	case "mongodb":
		entries, err := os.ReadDir(job.Destination)
		if err != nil {
			return nil, err
		}

		for _, entry := range entries {
			if entry.IsDir() && strings.Contains(entry.Name(), "_") {
				backupFiles = append(backupFiles, filepath.Join(job.Destination, entry.Name()))
			}
		}

	case "file":
		entries, err := os.ReadDir(job.Destination)
		if err != nil {
			return nil, err
		}

		baseName := filepath.Base(job.Source)
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasPrefix(entry.Name(), baseName+"_") {
				backupFiles = append(backupFiles, filepath.Join(job.Destination, entry.Name()))
			}
		}

	case "directory":
		pattern := filepath.Join(job.Destination, "*.tar.gz")
		files, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		backupFiles = files
	}

	return backupFiles, nil
}

func (m model) View() string {
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")).
		Render("💾 Backup Manager - Job Scheduler")

	if len(m.jobs) == 0 {
		content := "\nNo backup jobs configured yet.\n\nPress 'n' to add your first backup job!"
		footer := "n: add new job • q: quit"
		return fmt.Sprintf("%s\n%s\n\n%s", header, content, footer)
	}

	var statusMessage string
	if m.statusMsg != "" && time.Now().Before(m.statusExpiry) {
		color := lipgloss.Color("86")
		if strings.Contains(m.statusMsg, "❌") || strings.Contains(m.statusMsg, "Failed") {
			color = lipgloss.Color("196")
		}
		statusStyle := lipgloss.NewStyle().Foreground(color)
		statusMessage = " > " + statusStyle.Render(m.statusMsg)
	}

	var footer string
	if m.editMode {
		colNames := []string{"", "Name", "Type", "Source", "Schedule"}
		colName := colNames[m.editCol]
		footer = fmt.Sprintf("Editing %s: %s | tab: next field • enter: save • esc: cancel", colName, m.textInput.View())
	} else {
		footer = fmt.Sprintf("↑↓: navigate • space/enter: backup • ctrl+r: restore • e: edit • n/a: add • p: pause/resume • d/delete: delete • r: refresh • q: quit\n%s", statusMessage)
	}

	tableView := m.table.View()
	return fmt.Sprintf("%s\n\n%s\n\n%s", header, tableView, footer)
}

// Backup functions
func backupPostgres(dbName, destination, timestamp string) error {
	filename := fmt.Sprintf("%s_%s.sql", dbName, timestamp)
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

func backupMySQL(dbName, destination, timestamp string) error {
	filename := fmt.Sprintf("%s_%s.sql", dbName, timestamp)
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

func backupFile(source, destination, timestamp string) error {
	basename := filepath.Base(source)
	filename := fmt.Sprintf("%s_%s", basename, timestamp)
	fullPath := filepath.Join(destination, filename)

	os.MkdirAll(destination, 0755)

	cmd := exec.Command("cp", source, fullPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}

	return cmd.Run()
}

func backupDirectory(source, destination, timestamp string) error {
	basename := filepath.Base(source)
	filename := fmt.Sprintf("%s_%s.tar.gz", basename, timestamp)
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
