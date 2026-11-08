package tui

import "github.com/charmbracelet/lipgloss"

// ANSI colour numbers, so the terminal's own theme decides the exact shade.
const (
	colorAccent = lipgloss.Color("6") // cyan
	colorOK     = lipgloss.Color("2")
	colorWarn   = lipgloss.Color("3")
	colorBad    = lipgloss.Color("1")
	colorTool   = lipgloss.Color("5") // magenta

	// a call sits in a band of its own, the way pi paints a tool execution: the band carries the
	// state, so what a call did is read before its words are. The terminal's own grey, green and red
	// keep it right on a light theme as well as a dark one.
	colorToolWait = lipgloss.Color("8")
	colorToolDone = lipgloss.Color("2")
	colorToolFail = lipgloss.Color("1")
	colorMuted    = lipgloss.Color("8")
)

var (
	styleDim      = lipgloss.NewStyle().Faint(true)
	styleThink    = lipgloss.NewStyle().Faint(true).Italic(true)
	styleBold     = lipgloss.NewStyle().Bold(true)
	styleAccent   = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleOK       = lipgloss.NewStyle().Foreground(colorOK)
	styleWarn     = lipgloss.NewStyle().Foreground(colorWarn)
	styleBad      = lipgloss.NewStyle().Foreground(colorBad)
	styleWorking  = lipgloss.NewStyle().Foreground(colorTool).Bold(true)
	styleToolWait = lipgloss.NewStyle().Background(colorToolWait)
	styleToolDone = lipgloss.NewStyle().Background(colorToolDone)
	styleToolFail = lipgloss.NewStyle().Background(colorToolFail)
	// styleUserLine is the band a prompt sits in: reversing the terminal's own colours keeps it
	// right on a light theme as well as a dark one.
	styleUserLine = lipgloss.NewStyle().Reverse(true).Bold(true)
	// a change reads at a glance: what came in is kept, what went is marked, and what changed inside
	// a line that stayed is reversed out.
	styleDiffAdd  = lipgloss.NewStyle().Foreground(colorOK)
	styleDiffDel  = lipgloss.NewStyle().Foreground(colorBad)
	styleDiffMark = lipgloss.NewStyle().Reverse(true)
	styleCode     = lipgloss.NewStyle().Foreground(colorAccent)
	styleItalic   = lipgloss.NewStyle().Italic(true)
	styleKey      = lipgloss.NewStyle().Foreground(colorAccent)
)
