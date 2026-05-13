package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/LFroesch/backup-xd/suitechrome"
	"github.com/charmbracelet/lipgloss"
)

func appTitle() string {
	return suitechrome.RenderTitle("backup-xd", version)
}

func (m model) View() string {
	if m.showHelp {
		return m.renderHelp()
	}
	switch m.screen {
	case screenMain:
		return m.renderMain()
	case screenBackupManagement:
		return m.renderBackupManagement()
	case screenGlobalBackups:
		return m.renderGlobalBackups()
	case screenBackupView:
		return m.renderBackupView()
	case screenBackupClean:
		return m.renderBackupClean()
	default:
		return "Screen not implemented yet"
	}
}

func (m model) renderHelp() string {
	helpWidth := max(28, m.width-4)
	helpStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary).
		Padding(1, 2).
		Width(helpWidth)

	var lines []string
	lines = append(lines, appTitle()+dimTextStyle.Render(" — Help"))
	lines = append(lines, "")
	lines = append(lines, dimTextStyle.Render("Main"))
	lines = append(lines, fmt.Sprintf("  %s  %s", keyStyle.Render(fmt.Sprintf("%-16s", "j/k, ↑/↓")), "Navigate"))
	lines = append(lines, fmt.Sprintf("  %s  %s", keyStyle.Render(fmt.Sprintf("%-16s", "enter")), "Select menu item"))
	lines = append(lines, fmt.Sprintf("  %s  %s", keyStyle.Render(fmt.Sprintf("%-16s", "?")), "Open help"))
	lines = append(lines, fmt.Sprintf("  %s  %s", keyStyle.Render(fmt.Sprintf("%-16s", "q / esc")), "Back or quit"))
	lines = append(lines, "")
	lines = append(lines, dimTextStyle.Render("Backup Management"))
	lines = append(lines, fmt.Sprintf("  %s  %s", keyStyle.Render(fmt.Sprintf("%-16s", "a / n")), "Add new job"))
	lines = append(lines, fmt.Sprintf("  %s  %s", keyStyle.Render(fmt.Sprintf("%-16s", "e")), "Edit selected job"))
	lines = append(lines, fmt.Sprintf("  %s  %s", keyStyle.Render(fmt.Sprintf("%-16s", "enter / space")), "Run selected job now"))
	lines = append(lines, fmt.Sprintf("  %s  %s", keyStyle.Render(fmt.Sprintf("%-16s", "p")), "Pause or resume selected job"))
	lines = append(lines, fmt.Sprintf("  %s  %s", keyStyle.Render(fmt.Sprintf("%-16s", "r")), "Reload jobs"))
	lines = append(lines, fmt.Sprintf("  %s  %s", keyStyle.Render(fmt.Sprintf("%-16s", "delete")), "Delete selected job"))
	lines = append(lines, fmt.Sprintf("  %s  %s", keyStyle.Render(fmt.Sprintf("%-16s", "ctrl+r")), "Restore latest backup (confirm first)"))
	lines = append(lines, "")
	lines = append(lines, dimTextStyle.Render("Global / Cleanup"))
	lines = append(lines, fmt.Sprintf("  %s  %s", keyStyle.Render(fmt.Sprintf("%-16s", "v / d")), "View or delete a backup"))
	lines = append(lines, fmt.Sprintf("  %s  %s", keyStyle.Render(fmt.Sprintf("%-16s", "c")), "Open cleanup confirmation"))
	lines = append(lines, fmt.Sprintf("  %s  %s", keyStyle.Render(fmt.Sprintf("%-16s", "+ / -")), "Adjust cleanup retention days"))
	lines = append(lines, fmt.Sprintf("  %s  %s", keyStyle.Render(fmt.Sprintf("%-16s", "y / n")), "Confirm or cancel destructive actions"))
	lines = append(lines, "")
	lines = append(lines, dimTextStyle.Render("Press ?, q, or esc to close."))

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		helpStyle.Render(strings.Join(lines, "\n")))
}

func (m model) renderMain() string {
	options := []string{
		"Backup Management",
		"View All Backups (Global)",
		"Cleanup Old Backups",
		"Quit",
	}

	header := appTitle()

	var rows []string
	for i, option := range options {
		if i == m.cursor {
			rows = append(rows, selectedStyle.Render("> "+option))
		} else {
			rows = append(rows, normalStyle.Render("  "+option))
		}
	}

	status := suitechrome.RenderActions([]suitechrome.Action{
		{Key: "enter", Label: "select"},
		{Key: "j/k", Label: "navigate"},
		{Key: "q", Label: "quit"},
	})

	messageStr := ""
	if m.message != "" {
		messageStr = "\n" + statusMsgStyle.Render("> "+m.message)
	}

	return fmt.Sprintf("%s\n\n%s\n\n%s%s", header, strings.Join(rows, "\n"), status, messageStr)
}

func (m model) renderBackupManagement() string {
	if len(m.jobs) == 0 {
		content := dimTextStyle.Render("No backup jobs configured yet. Press 'a' to add one.")
		footer := suitechrome.RenderActions([]suitechrome.Action{
			{Key: "a", Label: "add"},
			{Key: "esc", Label: "back"},
		})
		return lipgloss.JoinVertical(lipgloss.Left, content, "", footer)
	}

	// Stats
	activeJobs, completedJobs, pausedJobs := 0, 0, 0
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

	title := appTitle()

	statsBox := lipgloss.NewStyle().
		Foreground(colorDim).
		Render(fmt.Sprintf("%d total  %d active  %d paused  %d done",
			len(m.jobs), activeJobs, pausedJobs, completedJobs))

	header := lipgloss.JoinHorizontal(lipgloss.Bottom, title, "  ", statsBox)

	// Status message
	var statusLine string
	if m.statusMsg != "" && time.Now().Before(m.statusExpiry) {
		if strings.Contains(m.statusMsg, "Failed") || strings.Contains(m.statusMsg, "Error") {
			statusLine = errorTextStyle.Render("> " + m.statusMsg)
		} else {
			statusLine = statusMsgStyle.Render("> " + m.statusMsg)
		}
	}

	// Footer
	var footer string
	if m.deleteMode {
		footer = errorTextStyle.Render(fmt.Sprintf("Delete '%s'? ", m.deleteTargetName)) +
			dimTextStyle.Render("y: yes  n/esc: no")
	} else if m.editMode {
		colNames := []string{"", "Name", "Type", "Source", "Schedule"}
		colName := colNames[m.editCol]
		scheduleHelp := ""
		if colName == "Schedule" {
			scheduleHelp = " (1h, 24h, 7d, oneoff)"
		}
		footer = warnTextStyle.Render(fmt.Sprintf("Editing %s%s: ", colName, scheduleHelp)) +
			m.textInput.View() +
			"\n" + dimTextStyle.Render("tab: next field  enter: save  esc: cancel")
	} else if m.restoreConfirm {
		footer = warnTextStyle.Render(fmt.Sprintf("Restore latest backup for '%s'? ", m.restoreTargetName)) +
			dimTextStyle.Render("y: yes  n/esc: no")
	} else {
		var actions []suitechrome.Action
		add := func(key, action string) {
			actions = append(actions, suitechrome.Action{Key: key, Label: action})
		}
		add("enter", "backup")
		add("ctrl+r", "restore")
		add("e", "edit")
		add("a", "add")
		add("p", "pause")
		add("del", "delete")
		add("r", "refresh")
		add("esc", "back")
		footer = suitechrome.RenderActions(actions)
	}

	var viewParts []string
	viewParts = append(viewParts, header, "", m.table.View())
	if statusLine != "" {
		viewParts = append(viewParts, statusLine)
	}
	viewParts = append(viewParts, "", footer)

	return lipgloss.JoinVertical(lipgloss.Left, viewParts...)
}

func (m model) renderGlobalBackups() string {
	if len(m.globalBackups) == 0 {
		return appTitle() + dimTextStyle.Render(" — Global Backups") + "\n\n" +
			dimTextStyle.Render("No backups found.") + "\n\n" +
			suitechrome.RenderActions([]suitechrome.Action{{Key: "esc", Label: "back"}})
	}

	header := appTitle() + dimTextStyle.Render(fmt.Sprintf("  ·  Global Backups (%d total)", len(m.globalBackups)))

	var rows []string
	for i, backup := range m.globalBackups {
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

	var footer string
	if m.globalDeleteMode {
		footer = errorTextStyle.Render(fmt.Sprintf("Delete backup '%s'? ", m.globalDeleteTargetName)) +
			dimTextStyle.Render("y: yes  n/esc: no")
	} else {
		footer = suitechrome.RenderActions([]suitechrome.Action{
			{Key: "v", Label: "view"},
			{Key: "d", Label: "delete"},
			{Key: "esc", Label: "back"},
		})
	}

	messageStr := ""
	if m.message != "" {
		messageStr = "\n" + statusMsgStyle.Render("> "+m.message)
	}

	return fmt.Sprintf("%s\n\n%s\n\n%s%s", header, strings.Join(rows, "\n"), footer, messageStr)
}

func (m model) renderBackupView() string {
	if m.selectedBackup == nil {
		return appTitle() + dimTextStyle.Render(" — Backup Details") + "\n\n" +
			errorTextStyle.Render("No backup selected.") + "\n\n" +
			suitechrome.RenderActions([]suitechrome.Action{{Key: "esc", Label: "back"}})
	}

	backup := m.selectedBackup
	backupPath := filepath.Join(getBackupBaseDir(), "backup-xd", backupTypeDir(backup.BackupType), backup.BackupFile)

	lines := []string{
		appTitle() + dimTextStyle.Render(" — Backup Details"),
		"",
		fmt.Sprintf("%s %s", keyStyle.Render("Job"), backup.JobName),
		fmt.Sprintf("%s %d", keyStyle.Render("Job ID"), backup.JobID),
		fmt.Sprintf("%s %s", keyStyle.Render("Type"), backup.BackupType),
		fmt.Sprintf("%s %s", keyStyle.Render("Status"), backup.Status),
		fmt.Sprintf("%s %s", keyStyle.Render("Source"), backup.Source),
		fmt.Sprintf("%s %s", keyStyle.Render("Created"), backup.Timestamp.Format("2006-01-02 15:04:05")),
		fmt.Sprintf("%s %s ago", keyStyle.Render("Age"), formatAge(backup.Timestamp)),
		fmt.Sprintf("%s %s", keyStyle.Render("Size"), backup.FileSizeStr),
		fmt.Sprintf("%s %s", keyStyle.Render("Duration"), backup.Duration),
		fmt.Sprintf("%s %s", keyStyle.Render("Storage"), backup.BackupFile),
		fmt.Sprintf("%s %s", keyStyle.Render("Path"), backupPath),
	}

	if backup.Error != "" {
		lines = append(lines, fmt.Sprintf("%s %s", errorTextStyle.Render("Error"), backup.Error))
	}

	footer := suitechrome.RenderActions([]suitechrome.Action{
		{Key: "esc", Label: "back"},
		{Key: "q", Label: "back"},
	})

	return strings.Join(append(lines, "", footer), "\n")
}

func (m model) renderBackupClean() string {
	header := appTitle() + dimTextStyle.Render("  ·  Cleanup Old Backups")

	settingsText := fmt.Sprintf("Cleanup backups older than: %d days\n\n", m.cleanupDays)

	if len(m.cleanupPreview) > 0 {
		settingsText += fmt.Sprintf("Found %d backups to delete:\n", len(m.cleanupPreview))

		var totalSize int64
		for _, backup := range m.cleanupPreview {
			settingsText += fmt.Sprintf("  %s (%s) - %s\n",
				backup.JobName,
				backup.FileSizeStr,
				formatAge(backup.Timestamp))
			totalSize += backup.FileSize
		}

		settingsText += fmt.Sprintf("\nTotal space to reclaim: %s\n", formatFileSize(totalSize))

		if m.cleanupConfirm {
			settingsText += "\n" + warnTextStyle.Render("Are you sure? This cannot be undone!")
		}
	} else {
		settingsText += dimTextStyle.Render("No backups older than this period found.")
	}

	var footer string
	if m.cleanupConfirm {
		footer = suitechrome.RenderActions([]suitechrome.Action{
			{Key: "y", Label: "confirm"},
			{Key: "n/esc", Label: "cancel"},
		})
	} else {
		footer = suitechrome.RenderActions([]suitechrome.Action{
			{Key: "+/-", Label: "adjust days"},
			{Key: "c", Label: "clean"},
			{Key: "esc", Label: "back"},
		})
	}

	messageStr := ""
	if m.message != "" {
		messageStr = "\n" + statusMsgStyle.Render("> "+m.message)
	}

	return fmt.Sprintf("%s\n\n%s\n%s%s", header, settingsText, footer, messageStr)
}
