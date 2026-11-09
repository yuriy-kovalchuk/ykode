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
	// a change marks its side with one coloured glyph. The words on the row keep the terminal's own
	// colour: a whole line of the terminal's red or green is noise, and nothing on it reads.
	styleAddMark = lipgloss.NewStyle().Foreground(colorOK)
	styleDelMark = lipgloss.NewStyle().Foreground(colorBad)
	styleCode    = lipgloss.NewStyle().Foreground(colorAccent)
	styleItalic  = lipgloss.NewStyle().Italic(true)
	styleKey     = lipgloss.NewStyle().Foreground(colorAccent)
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

// diffSides are the grounds a change sits on: the side that came in, the side that went, and the
// stronger shade for the words inside a line that changed. As with a call's ground, these are picked
// for the terminal the screen runs in, and low enough that the words stay the readable part.
type diffSurfaces struct {
	add, del, addMark, delMark lipgloss.Style
}

var diffSides = sync.OnceValue(func() diffSurfaces {
	dark := lipgloss.HasDarkBackground()
	return diffSurfaces{
		add:     ground(dark, "#1d2b21", "#eef6ef"),
		del:     ground(dark, "#2b1e1f", "#f7eecd"),
		addMark: ground(dark, "#3f5a45", "#cfe6d3"),
		delMark: ground(dark, "#57332f", "#f0cfc4"),
	}
})

// ground is one surface: the dark terminal's shade or the light terminal's.
func ground(isDark bool, darkShade, lightShade string) lipgloss.Style {
	if isDark {
		return lipgloss.NewStyle().Background(lipgloss.Color(darkShade))
	}
	return lipgloss.NewStyle().Background(lipgloss.Color(lightShade))
}
