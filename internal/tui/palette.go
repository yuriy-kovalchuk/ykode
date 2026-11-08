package tui

import "github.com/charmbracelet/lipgloss"

// ANSI colour numbers, so the terminal's own theme decides the exact shade.
const (
	colorAccent = lipgloss.Color("6") // cyan
	colorOK     = lipgloss.Color("2")
	colorWarn   = lipgloss.Color("3")
	colorBad    = lipgloss.Color("1")
	colorTool   = lipgloss.Color("5") // magenta
	colorMuted  = lipgloss.Color("8")
)

var (
	styleDim      = lipgloss.NewStyle().Faint(true)
	styleThink    = lipgloss.NewStyle().Faint(true).Italic(true)
	styleBold     = lipgloss.NewStyle().Bold(true)
	styleAccent   = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleOK       = lipgloss.NewStyle().Foreground(colorOK)
	styleWarn     = lipgloss.NewStyle().Foreground(colorWarn)
	styleBad      = lipgloss.NewStyle().Foreground(colorBad)
	styleToolName = lipgloss.NewStyle().Foreground(colorTool).Bold(true)
	styleWorking  = lipgloss.NewStyle().Foreground(colorTool).Bold(true)
	styleUserBar  = lipgloss.NewStyle().Foreground(colorAccent)
	styleKey      = lipgloss.NewStyle().Foreground(colorAccent)
)
