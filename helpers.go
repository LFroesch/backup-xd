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
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- Config / IO ---

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
		return
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
		}
		saveConfig(config, configFile)
		return config
	}

	if err := json.Unmarshal(data, &config); err != nil {
		log.Printf("Warning: could not parse config: %v", err)
	}
	return config
}

func saveConfig(config BackupConfig, configFile string) error {
	if err := os.MkdirAll(filepath.Dir(configFile), 0755); err != nil {
		return err
	}
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
		return "./backups"
	}
	return filepath.Join(homeDir, "backups")
}

// --- Table management ---

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
	tableHeight := max(m.height-6, 5)

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

// --- Edit helpers ---

func (m *model) startEdit() {
	if len(m.jobs) == 0 {
		return
	}

	m.editMode = true
	m.editRow = m.table.Cursor()
	m.editCol = 1
	m.loadEditField()
}

func (m *model) loadEditField() {
	if m.editRow < 0 || m.editRow >= len(m.jobs) {
		return
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

// --- Global backup scanning ---

func scanGlobalBackups() []BackupMetadata {
	backupBaseDir := getBackupBaseDir()
	var allBackups []BackupMetadata

	backupTypes := []string{"postgres", "mysql", "mongodb", "files", "directories"}

	for _, backupType := range backupTypes {
		typeDir := filepath.Join(backupBaseDir, "backup-xd", backupType)

		entries, err := os.ReadDir(typeDir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			metadataPath := filepath.Join(typeDir, entry.Name(), "metadata.json")
			if metadata, err := loadBackupMetadata(metadataPath); err == nil {
				allBackups = append(allBackups, metadata)
			}
		}
	}

	sort.Slice(allBackups, func(i, j int) bool {
		return allBackups[i].Timestamp.After(allBackups[j].Timestamp)
	})

	return allBackups
}

func loadBackupMetadata(metadataPath string) (BackupMetadata, error) {
	var metadata BackupMetadata
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return metadata, err
	}
	err = json.Unmarshal(data, &metadata)
	return metadata, err
}

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

func saveBackupMetadata(metadata BackupMetadata, metadataFile string) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metadataFile, data, 0644)
}

// --- Cleanup / Delete ---

func (m model) performGlobalBackupDelete() (model, tea.Cmd) {
	if m.globalDeleteTargetIdx >= 0 && m.globalDeleteTargetIdx < len(m.globalBackups) {
		backup := m.globalBackups[m.globalDeleteTargetIdx]
		backupBaseDir := getBackupBaseDir()

		dirName := backup.BackupType
		switch backup.BackupType {
		case "file":
			dirName = "files"
		case "directory":
			dirName = "directories"
		}

		backupPath := filepath.Join(backupBaseDir, "backup-xd", dirName, backup.BackupFile)

		if err := os.RemoveAll(backupPath); err != nil {
			m.message = fmt.Sprintf("Failed to delete backup: %v", err)
		} else {
			m.message = fmt.Sprintf("Deleted backup: %s", backup.JobName)
			m.globalBackups = append(m.globalBackups[:m.globalDeleteTargetIdx], m.globalBackups[m.globalDeleteTargetIdx+1:]...)
			if m.cursor >= len(m.globalBackups) && len(m.globalBackups) > 0 {
				m.cursor = len(m.globalBackups) - 1
			} else if len(m.globalBackups) == 0 {
				m.cursor = 0
			}
		}
	}

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
		dirName := backup.BackupType
		switch backup.BackupType {
		case "file":
			dirName = "files"
		case "directory":
			dirName = "directories"
		}
		backupPath := filepath.Join(backupBaseDir, "backup-xd", dirName, backup.BackupFile)

		if err := os.RemoveAll(backupPath); err != nil {
			errors = append(errors, fmt.Sprintf("Failed to delete %s: %v", backup.JobName, err))
		} else {
			deletedCount++
			reclaimedSize += backup.FileSize
		}
	}

	m.globalBackups = scanGlobalBackups()
	m.cleanupPreview = getOldBackups(m.cleanupDays)
	m.cleanupConfirm = false

	if len(errors) > 0 {
		m.message = fmt.Sprintf("Deleted %d backups (%s reclaimed). Errors: %s",
			deletedCount, formatFileSize(reclaimedSize), strings.Join(errors, "; "))
	} else {
		m.message = fmt.Sprintf("Cleaned up %d backups, reclaimed %s",
			deletedCount, formatFileSize(reclaimedSize))
	}

	return m, nil
}

// --- Backup execution ---

func (m model) runBackup(jobID int) tea.Cmd {
	return func() tea.Msg {
		for _, job := range m.jobs {
			if job.ID == jobID {
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
					if envVar, ok := strings.CutPrefix(connectionString, "$"); ok {
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
						message: fmt.Sprintf("Backup failed: %v", err),
					}
				}

				successMsg := fmt.Sprintf("Backup completed: %s", job.Name)
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
			message: "Job not found",
		}
	}
}

func (m model) showRestoreOptions(job BackupJob) tea.Cmd {
	return func() tea.Msg {
		backupFiles, err := findBackupsForJob(job)
		if err != nil {
			return statusMsg{message: fmt.Sprintf("Error finding backups: %v", err)}
		}

		if len(backupFiles) == 0 {
			return statusMsg{message: "No backups found for this job"}
		}

		latestBackup := backupFiles[len(backupFiles)-1]

		connectionString := job.Source
		if envVar, ok := strings.CutPrefix(connectionString, "$"); ok {
			connectionString = os.Getenv(envVar)
			if connectionString == "" {
				return statusMsg{message: fmt.Sprintf("Environment variable %s not set", envVar)}
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
			return statusMsg{message: fmt.Sprintf("Restore failed: %v", err2)}
		}

		return statusMsg{message: fmt.Sprintf("Restored: %s from %s", job.Name, filepath.Base(latestBackup))}
	}
}

func findBackupsForJob(job BackupJob) ([]string, error) {
	var backupFiles []string

	backupBaseDir := getBackupBaseDir()
	var expandedDestination string

	if strings.HasPrefix(job.Destination, backupBaseDir) {
		expandedDestination = job.Destination
	} else {
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
		entries, err := os.ReadDir(expandedDestination)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), strings.ReplaceAll(job.Name, " ", "_")+"_") {
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
		entries, err := os.ReadDir(expandedDestination)
		if err != nil {
			return nil, err
		}
		expandedSource := expandPath(job.Source)
		baseName := filepath.Base(expandedSource)
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), strings.ReplaceAll(job.Name, " ", "_")+"_") {
				backupFile := filepath.Join(expandedDestination, entry.Name(), baseName)
				if _, err := os.Stat(backupFile); err == nil {
					backupFiles = append(backupFiles, backupFile)
				}
			}
		}

	case "directory":
		entries, err := os.ReadDir(expandedDestination)
		if err != nil {
			return nil, err
		}
		expandedSource := expandPath(job.Source)
		baseName := filepath.Base(expandedSource)
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), strings.ReplaceAll(job.Name, " ", "_")+"_") {
				tarFile := filepath.Join(expandedDestination, entry.Name(), baseName+".tar.gz")
				if _, err := os.Stat(tarFile); err == nil {
					backupFiles = append(backupFiles, tarFile)
				}
			}
		}
	}

	return backupFiles, nil
}

// --- Backup providers ---

func backupPostgresWithMetadata(job BackupJob, timestamp string, startTime time.Time) (string, error) {
	backupBaseDir := getBackupBaseDir()
	backupDir := filepath.Join(backupBaseDir, "backup-xd", "postgres")
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
		JobID: job.ID, JobName: job.Name, BackupType: job.Type, Source: job.Source,
		BackupFile: folderName, Timestamp: startTime,
		HumanTime: startTime.Format("January 2, 2006 at 3:04 PM"),
		FileSize: fileSize, FileSizeStr: formatFileSize(fileSize),
		Duration: duration.String(), Status: "completed",
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
	if pgHost == "" { pgHost = "localhost" }
	if pgUser == "" { pgUser = "postgres" }
	if pgPassword == "" { pgPassword = "postgres" }
	if pgPort == "" { pgPort = "5432" }

	cmd := exec.Command("pg_dump", "-h", pgHost, "-U", pgUser, "-p", pgPort, "-d", dbName, "-f", fullPath)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+pgPassword)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}
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
		JobID: job.ID, JobName: job.Name, BackupType: job.Type, Source: job.Source,
		BackupFile: folderName, Timestamp: startTime,
		HumanTime: startTime.Format("January 2, 2006 at 3:04 PM"),
		FileSize: fileSize, FileSizeStr: formatFileSize(fileSize),
		Duration: duration.String(), Status: "completed",
	}
	saveBackupMetadata(metadata, filepath.Join(folderPath, "metadata.json"))
	return folderPath, nil
}

func backupMySQL(dbName, destination, timestamp string) error {
	filename := fmt.Sprintf("%s.sql", dbName)
	fullPath := filepath.Join(destination, filename)
	os.MkdirAll(destination, 0755)

	mysqlHost := os.Getenv("MYSQL_HOST")
	mysqlUser := os.Getenv("MYSQL_USER")
	mysqlPassword := os.Getenv("MYSQL_PASSWORD")
	mysqlPort := os.Getenv("MYSQL_PORT")
	if mysqlHost == "" { mysqlHost = "localhost" }
	if mysqlUser == "" { mysqlUser = "root" }
	if mysqlPort == "" { mysqlPort = "3306" }

	args := []string{"-h", mysqlHost, "-u", mysqlUser, "-P", mysqlPort}
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
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}
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
		JobID: job.ID, JobName: job.Name, BackupType: job.Type, Source: job.Source,
		BackupFile: dirname, Timestamp: startTime,
		HumanTime: startTime.Format("January 2, 2006 at 3:04 PM"),
		FileSize: fileSize, FileSizeStr: formatFileSize(fileSize),
		Duration: duration.String(), Status: "completed",
	}
	saveBackupMetadata(metadata, filepath.Join(dirpath, "metadata.json"))
	return dirpath, nil
}

func backupMongoDB(connectionString, destination, timestamp, jobName string) error {
	dirname := fmt.Sprintf("%s_%s", strings.ReplaceAll(jobName, " ", "_"), timestamp)
	dirpath := filepath.Join(destination, dirname)
	os.MkdirAll(destination, 0755)

	cmd := exec.Command("mongodump", "--uri", connectionString, "--out", dirpath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}
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
		JobID: job.ID, JobName: job.Name, BackupType: job.Type, Source: job.Source,
		BackupFile: folderName, Timestamp: startTime,
		HumanTime: startTime.Format("January 2, 2006 at 3:04 PM"),
		FileSize: fileSize, FileSizeStr: formatFileSize(fileSize),
		Duration: duration.String(), Status: "completed",
	}
	saveBackupMetadata(metadata, filepath.Join(folderPath, "metadata.json"))
	return folderPath, nil
}

func backupFile(source, destination, timestamp string) error {
	basename := filepath.Base(source)
	fullPath := filepath.Join(destination, basename)
	os.MkdirAll(destination, 0755)
	cmd := exec.Command("cp", source, fullPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}
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
		JobID: job.ID, JobName: job.Name, BackupType: job.Type, Source: job.Source,
		BackupFile: folderName, Timestamp: startTime,
		HumanTime: startTime.Format("January 2, 2006 at 3:04 PM"),
		FileSize: fileSize, FileSizeStr: formatFileSize(fileSize),
		Duration: duration.String(), Status: "completed",
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
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}
	return cmd.Run()
}

// --- Restore ---

func restorePostgres(dbName, backupFile string) error {
	pgHost := os.Getenv("PGHOST")
	pgUser := os.Getenv("PGUSER")
	pgPassword := os.Getenv("PGPASSWORD")
	pgPort := os.Getenv("PGPORT")
	if pgHost == "" { pgHost = "localhost" }
	if pgUser == "" { pgUser = "postgres" }
	if pgPassword == "" { pgPassword = "postgres" }
	if pgPort == "" { pgPort = "5432" }

	cmd := exec.Command("psql", "-h", pgHost, "-U", pgUser, "-p", pgPort, "-d", dbName, "-f", backupFile)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+pgPassword)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}
	return cmd.Run()
}

func restoreMongoDB(connectionString, backupDir string) error {
	cmd := exec.Command("mongorestore", "--uri", connectionString, "--drop", backupDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mongorestore failed: %v\nOutput: %s", err, string(output))
	}
	return nil
}

func restoreFile(originalPath, backupFile string) error {
	cmd := exec.Command("cp", backupFile, originalPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}
	return cmd.Run()
}

func restoreDirectory(originalPath, backupFile string) error {
	cmd := exec.Command("tar", "-xzf", backupFile, "-C", filepath.Dir(originalPath))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: 0}
	return cmd.Run()
}

// --- Utility ---

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

func formatAge(timestamp time.Time) string {
	age := time.Since(timestamp)
	if age < time.Hour {
		return fmt.Sprintf("%dm", int(age.Minutes()))
	} else if age < 24*time.Hour {
		return fmt.Sprintf("%dh", int(age.Hours()))
	}
	return fmt.Sprintf("%dd", int(age.Hours()/24))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// --- Table styling ---

func styledTable(t table.Model) table.Model {
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorDim).
		BorderBottom(true).
		Bold(true).
		Foreground(colorAccent).
		Align(lipgloss.Left).
		PaddingLeft(0)
	s.Selected = s.Selected.
		Foreground(colorText).
		Background(lipgloss.Color("#3A3A5C")).
		Bold(true).
		Align(lipgloss.Left).
		PaddingLeft(0)
	s.Cell = s.Cell.
		Align(lipgloss.Left).
		PaddingLeft(0)
	t.SetStyles(s)
	return t
}
