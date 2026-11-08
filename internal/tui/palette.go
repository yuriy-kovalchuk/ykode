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
	// styleUserLine is the band a prompt sits in: reversing the terminal's own colours keeps it
	// right on a light theme as well as a dark one.
	styleUserLine = lipgloss.NewStyle().Reverse(true).Bold(true)
	styleResult   = lipgloss.NewStyle() // what a call answered is shown as the tool wrote it
	// a change reads at a glance: what came in is kept, what went is marked, and what changed inside
	// a line that stayed is reversed out.
	styleDiffAdd  = lipgloss.NewStyle().Foreground(colorOK)
	styleDiffDel  = lipgloss.NewStyle().Foreground(colorBad)
	styleDiffMark = lipgloss.NewStyle().Reverse(true)
	styleCode     = lipgloss.NewStyle().Foreground(colorAccent)
	styleItalic   = lipgloss.NewStyle().Italic(true)
	styleKey      = lipgloss.NewStyle().Foreground(colorAccent)
)
