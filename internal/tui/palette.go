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
	styleKey     = lipgloss.NewStyle().Foreground(colorAccent)
	// A block of code marks only what a reader is looking for and leaves the rest the terminal's own
	// colour: a block whose every word is coloured is noise, and the punctuation of it reads as nothing.
	// Each keeps the source's own tabs: a style that expanded them would rewrite the code a reader copies.
	styleCodeDefault = lipgloss.NewStyle().TabWidth(-1)
	styleCodeKeyword = lipgloss.NewStyle().TabWidth(-1).Foreground(colorTool).Bold(true)
	styleCodeString  = lipgloss.NewStyle().TabWidth(-1).Foreground(colorOK)
	styleCodeComment = lipgloss.NewStyle().TabWidth(-1).Foreground(colorMuted).Italic(true)
	styleCodeName    = lipgloss.NewStyle().TabWidth(-1).Foreground(colorAccent)
)

// diffSurfaces are the grounds a change sits on: a shade for the side that came in and one for the
// side that went, and a stronger shade of each for the words that differ inside a line. They are
// picked for the terminal the screen runs in: deep on a dark one, pale on a light one, so the words
// on them keep the terminal's own colour and stay readable.
type diffSurfaces struct{ add, del, addWord, delWord lipgloss.Style }

var diffSides = sync.OnceValue(func() diffSurfaces {
	dark := lipgloss.HasDarkBackground()
	return diffSurfaces{
		add:     ground(dark, "#1f4d2a", "#d9f2dd"),
		del:     ground(dark, "#5c2630", "#fbdfe2"),
		addWord: ground(dark, "#2f7d45", "#a4e0af"),
		delWord: ground(dark, "#93404d", "#f3adb5"),
	}
})

func ground(isDark bool, darkShade, lightShade string) lipgloss.Style {
	if isDark {
		return lipgloss.NewStyle().Background(lipgloss.Color(darkShade))
	}
	return lipgloss.NewStyle().Background(lipgloss.Color(lightShade))
}
