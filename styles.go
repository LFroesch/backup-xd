package main

import "github.com/charmbracelet/lipgloss"

var (
	// Colors — consistent palette (matches portmon/chunes)
	colorPrimary = lipgloss.Color("#5AF78E") // green
	colorAccent  = lipgloss.Color("#57C7FF") // blue
	colorWarn    = lipgloss.Color("#FF6AC1") // pink
	colorError   = lipgloss.Color("#FF5C57") // red
	colorDim     = lipgloss.Color("#606060")
	colorText    = lipgloss.Color("#EEEEEE")

	// Header
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary)

	// Status bar keys
	keyStyle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	actionStyle = lipgloss.NewStyle().
			Foreground(colorPrimary)

	bulletStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	dimTextStyle = lipgloss.NewStyle().
			Foreground(colorDim)

	errorTextStyle = lipgloss.NewStyle().
			Foreground(colorError).
			Bold(true)

	successTextStyle = lipgloss.NewStyle().
				Foreground(colorPrimary).
				Bold(true)

	warnTextStyle = lipgloss.NewStyle().
			Foreground(colorWarn).
			Bold(true)

	statusMsgStyle = lipgloss.NewStyle().
			Foreground(colorAccent)

	selectedStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#3A3A5C")).
			Foreground(colorText)

	normalStyle = lipgloss.NewStyle().
			Foreground(colorText)
)
