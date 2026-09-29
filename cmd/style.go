package cmd

import "charm.land/lipgloss/v2"

// Basic ANSI colours, so the output follows the terminal's own theme.
var (
	styleOK      = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleWarn    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleError   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleBranch  = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	styleGB      = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	styleDim     = lipgloss.NewStyle().Faint(true)
	styleHeading = lipgloss.NewStyle().Bold(true)
)
