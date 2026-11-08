package tui

import (
	"sync"

	"github.com/charmbracelet/lipgloss"
)

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
	styleDim     = lipgloss.NewStyle().Faint(true)
	styleThink   = lipgloss.NewStyle().Faint(true).Italic(true)
	styleBold    = lipgloss.NewStyle().Bold(true)
	styleAccent  = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleOK      = lipgloss.NewStyle().Foreground(colorOK)
	styleWarn    = lipgloss.NewStyle().Foreground(colorWarn)
	styleBad     = lipgloss.NewStyle().Foreground(colorBad)
	styleWorking = lipgloss.NewStyle().Foreground(colorTool).Bold(true)
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

// toolSurfaces are the grounds a call sits in, the way pi paints a tool execution: the ground
// carries the state, so what a call did is read before its words are. pi's grounds are low and
// unsaturated for a reason - a whole row of the terminal's green or red is noise, and nothing sits
// readable on it - so a call takes a dark tint on a dark terminal, a pale one on a light one, and
// the words on it stay the terminal's own colour.
type toolSurfaces struct {
	wait, done, fail lipgloss.Style
}

var surfaces = sync.OnceValue(func() toolSurfaces {
	dark := lipgloss.HasDarkBackground()
	return toolSurfaces{
		wait: ground(dark, "#33343a", "#ececec"),
		done: ground(dark, "#24402f", "#e3efe4"),
		fail: ground(dark, "#4c2b26", "#f7e3df"),
	}
})

// ground is one surface: the dark terminal's shade or the light terminal's.
func ground(isDark bool, darkShade, lightShade string) lipgloss.Style {
	if isDark {
		return lipgloss.NewStyle().Background(lipgloss.Color(darkShade))
	}
	return lipgloss.NewStyle().Background(lipgloss.Color(lightShade))
}
