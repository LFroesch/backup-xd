package main

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

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
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.showHelp {
			if msg.String() == "?" || msg.String() == "q" || msg.String() == "esc" {
				m.showHelp = false
			}
			return m, nil
		}
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
		jobName := m.deleteTargetName
		m.jobs = append(m.jobs[:m.deleteTargetIdx], m.jobs[m.deleteTargetIdx+1:]...)
		m.config.Jobs = m.jobs
		saveConfig(m.config, m.configFile)
		m.updateTable()
		m.deleteMode = false
		m.deleteTargetIdx = -1
		m.deleteTargetName = ""
		return m, showStatus(fmt.Sprintf("Deleted %s", jobName))
	case "n", "N", "esc":
		m.deleteMode = false
		m.deleteTargetIdx = -1
		m.deleteTargetName = ""
		return m, nil
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
		return m, showStatus("Job updated")
	case "tab":
		m.saveEdit()
		m.editCol++
		if m.editCol > 4 {
			m.editCol = 1
		}
		m.loadEditField()
		return m, nil
	case "shift+tab":
		m.saveEdit()
		m.editCol--
		if m.editCol < 1 {
			m.editCol = 4
		}
		m.loadEditField()
		return m, nil
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "?":
		m.showHelp = true
		return m, nil
	case "q":
		if m.screen != screenMain {
			m.screen = screenMain
			m.cursor = 0
			return m, nil
		}
		return m, tea.Quit
	case "up", "k":
		if m.globalDeleteMode {
			return m, nil
		}
		if m.screen == screenBackupManagement {
			var cmd tea.Cmd
			m.table, cmd = m.table.Update(msg)
			return m, cmd
		}
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case "down", "j":
		if m.globalDeleteMode {
			return m, nil
		}
		if m.screen == screenBackupManagement {
			var cmd tea.Cmd
			m.table, cmd = m.table.Update(msg)
			return m, cmd
		}
		maxCursor := m.getMaxCursor()
		if m.cursor < maxCursor {
			m.cursor++
		}
		return m, nil
	case "enter":
		if m.screen == screenBackupManagement {
			if len(m.jobs) > 0 {
				job := m.jobs[m.table.Cursor()]
				return m, m.runBackup(job.ID)
			}
			return m, nil
		}
		return m.handleEnter()
	case "esc":
		if m.globalDeleteMode {
			m.globalDeleteMode = false
			m.globalDeleteTargetIdx = -1
			m.globalDeleteTargetName = ""
			return m, nil
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
	case "e":
		if m.screen == screenBackupManagement {
			m.startEdit()
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
			return m, showStatus("New backup job added")
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
			return m, showStatus("Refreshed")
		}
	case "p":
		if m.screen == screenBackupManagement && len(m.jobs) > 0 {
			idx := m.table.Cursor()
			currentStatus := m.jobs[idx].Status

			if currentStatus == "completed" && strings.ToLower(m.jobs[idx].Schedule) == "oneoff" {
				return m, showStatus("Cannot resume completed OneOff job")
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
			return m, showStatus(fmt.Sprintf("Job %s", m.jobs[idx].Status))
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

func (m model) handleEnter() (model, tea.Cmd) {
	switch m.screen {
	case screenMain:
		switch m.cursor {
		case 0:
			m.screen = screenBackupManagement
		case 1:
			m.screen = screenGlobalBackups
			m.globalBackups = scanGlobalBackups()
		case 2:
			m.screen = screenBackupClean
			m.cleanupPreview = getOldBackups(m.cleanupDays)
		case 3:
			m.screen = screenSettings
		case 4:
			return m, tea.Quit
		}
		m.cursor = 0
		m.message = ""
	}
	return m, nil
}

func (m model) getMaxCursor() int {
	switch m.screen {
	case screenMain:
		return 4
	case screenGlobalBackups:
		return len(m.globalBackups) - 1
	case screenBackupClean:
		return 0
	default:
		return 0
	}
}

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
