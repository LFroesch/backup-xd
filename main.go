package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

var version = "dev"

func NewModel() model {
	loadEnvironmentFile()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}

	configFile := filepath.Join(homeDir, ".config", "backup-xd", "config.json")
	config := loadBackupConfig(configFile)
	var changed bool
	config.Jobs, changed = normalizeJobsScheduleState(config.Jobs, time.Now())
	if changed {
		saveConfig(config, configFile)
	}

	m := model{
		jobs:          config.Jobs,
		config:        config,
		configFile:    configFile,
		screen:        screenMain,
		cursor:        0,
		width:         100,
		height:        24,
		editMode:      false,
		editRow:       -1,
		editCol:       -1,
		lastUpdate:    time.Now(),
		globalBackups: scanGlobalBackups(),
		cleanupDays:   30,
	}

	m.textInput = textinput.New()
	m.textInput.CharLimit = 200

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
	m.table = styledTable(t)
	m.updateTable()

	return m
}

func main() {
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "backup-xd — TUI backup manager for databases and filesystems\n\n")
		fmt.Fprintf(os.Stderr, "Usage: backup-xd [flags]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Println("backup-xd " + version)
		os.Exit(0)
	}

	m := NewModel()

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
