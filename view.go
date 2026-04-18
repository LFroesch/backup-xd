package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

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
	case screenBackupClean:
		return m.renderBackupClean()
	default:
		return "Screen not implemented yet"
	}
}

func (m model) renderHelp() string {
	helpStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary).
		Padding(1, 2).
		Width(m.width - 4)

	keys := []struct{ key, desc string }{
		{"j/k, ↑/↓", "Navigate"},
		{"enter", "Select / run backup"},
		{"e", "Edit selected job"},
		{"a", "Add new job"},
		{"delete", "Delete selected job"},
		{"esc/q", "Back / quit"},
		{"?", "Toggle this help"},
		{"ctrl+c", "Quit immediately"},
	}

	var lines []string
	lines = append(lines, titleStyle.Render("backup-xd — Help"))
	lines = append(lines, "")
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("  %s  %s",
			keyStyle.Render(fmt.Sprintf("%-16s", k.key)),
			k.desc,
		))
	}
	lines = append(lines, "")
	lines = append(lines, dimTextStyle.Render("Press ?, q, or esc to close"))

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

	header := titleStyle.Render("backup-xd")

	var rows []string
	for i, option := range options {
		if i == m.cursor {
			rows = append(rows, selectedStyle.Render("> "+option))
		} else {
			rows = append(rows, normalStyle.Render("  "+option))
		}
	}

	var status []string
	status = append(status,
		keyStyle.Render("enter"), " ", actionStyle.Render("select"),
		bulletStyle.Render(" · "),
		keyStyle.Render("j/k"), " ", actionStyle.Render("navigate"),
		bulletStyle.Render(" · "),
		keyStyle.Render("q"), " ", actionStyle.Render("quit"),
	)

	messageStr := ""
	if m.message != "" {
		messageStr = "\n" + statusMsgStyle.Render("> "+m.message)
	}

	return fmt.Sprintf("%s\n\n%s\n\n%s%s", header, strings.Join(rows, "\n"), strings.Join(status, ""), messageStr)
}

func (m model) renderBackupManagement() string {
	if len(m.jobs) == 0 {
		content := dimTextStyle.Render("No backup jobs configured yet. Press 'a' to add one.")
		footer := keyStyle.Render("a") + " " + actionStyle.Render("add") +
			bulletStyle.Render(" · ") +
			keyStyle.Render("esc") + " " + actionStyle.Render("back")
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

	title := titleStyle.Render("backup-xd")

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
	} else {
		var parts []string
		add := func(key, action string) {
			if len(parts) > 0 {
				parts = append(parts, bulletStyle.Render(" · "))
			}
			parts = append(parts, keyStyle.Render(key), " ", actionStyle.Render(action))
		}
		add("enter", "backup")
		add("ctrl+r", "restore")
		add("e", "edit")
		add("a", "add")
		add("p", "pause")
		add("del", "delete")
		add("r", "refresh")
		add("esc", "back")
		footer = strings.Join(parts, "")
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
		return titleStyle.Render("Global Backups") + "\n\n" +
			dimTextStyle.Render("No backups found.") + "\n\n" +
			keyStyle.Render("esc") + " " + actionStyle.Render("back")
	}

	header := titleStyle.Render(fmt.Sprintf("Global Backups (%d total)", len(m.globalBackups)))

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
		var parts []string
		add := func(key, action string) {
			if len(parts) > 0 {
				parts = append(parts, bulletStyle.Render(" · "))
			}
			parts = append(parts, keyStyle.Render(key), " ", actionStyle.Render(action))
		}
		add("v", "view")
		add("d", "delete")
		add("esc", "back")
		footer = strings.Join(parts, "")
	}

	messageStr := ""
	if m.message != "" {
		messageStr = "\n" + statusMsgStyle.Render("> "+m.message)
	}

	return fmt.Sprintf("%s\n\n%s\n\n%s%s", header, strings.Join(rows, "\n"), footer, messageStr)
}

func (m model) renderBackupClean() string {
	header := titleStyle.Render("Cleanup Old Backups")

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
		footer = keyStyle.Render("y") + " " + actionStyle.Render("confirm") +
			bulletStyle.Render(" · ") +
			keyStyle.Render("n/esc") + " " + actionStyle.Render("cancel")
	} else {
		var parts []string
		add := func(key, action string) {
			if len(parts) > 0 {
				parts = append(parts, bulletStyle.Render(" · "))
			}
			parts = append(parts, keyStyle.Render(key), " ", actionStyle.Render(action))
		}
		add("+/-", "adjust days")
		add("c", "clean")
		add("esc", "back")
		footer = strings.Join(parts, "")
	}

	messageStr := ""
	if m.message != "" {
		messageStr = "\n" + statusMsgStyle.Render("> "+m.message)
	}

	return fmt.Sprintf("%s\n\n%s\n%s%s", header, settingsText, footer, messageStr)
}
