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

// diffSurfaces are the grounds a change sits on: one low shade for the side that came in and one for
// the side that went, picked for the terminal the screen runs in (dark tints on a dark one, pale on a
// light one). Nothing stronger is laid over the words inside a line.
type diffSurfaces struct{ add, del lipgloss.Style }

var diffSides = sync.OnceValue(func() diffSurfaces {
	dark := lipgloss.HasDarkBackground()
	return diffSurfaces{
		add: ground(dark, "#1d2b21", "#eef6ef"),
		del: ground(dark, "#2b1e1f", "#f7e8e4"),
	}
})

func ground(isDark bool, darkShade, lightShade string) lipgloss.Style {
	if isDark {
		return lipgloss.NewStyle().Background(lipgloss.Color(darkShade))
	}
	return lipgloss.NewStyle().Background(lipgloss.Color(lightShade))
}
