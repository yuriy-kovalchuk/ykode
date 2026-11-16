package tui

import (
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
)

// ykode's own palette, a dark and a light shade of each, picked from the terminal's background.
var (
	colorAccent = lipgloss.AdaptiveColor{Dark: "#A78BFA", Light: "#6D28D9"} // violet
	colorCyan   = lipgloss.AdaptiveColor{Dark: "#67E8F9", Light: "#0E7490"}
	colorOK     = lipgloss.AdaptiveColor{Dark: "#4ADE80", Light: "#15803D"}
	colorWarn   = lipgloss.AdaptiveColor{Dark: "#FBBF24", Light: "#B45309"}
	colorBad    = lipgloss.AdaptiveColor{Dark: "#F87171", Light: "#B91C1C"}
	colorTool   = lipgloss.AdaptiveColor{Dark: "#F472B6", Light: "#BE185D"} // pink
	colorMuted  = lipgloss.AdaptiveColor{Dark: "#7C8394", Light: "#8A8F98"}
	colorThink  = lipgloss.AdaptiveColor{Dark: "#9D8CD6", Light: "#7C6BB8"}
	colorBorder = lipgloss.AdaptiveColor{Dark: "#454B61", Light: "#C9CCD6"}
)

var (
	styleDim     = lipgloss.NewStyle().Foreground(colorMuted)
	styleThink   = lipgloss.NewStyle().Foreground(colorThink).Italic(true)
	styleBold    = lipgloss.NewStyle().Bold(true)
	styleAccent  = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleOK      = lipgloss.NewStyle().Foreground(colorOK)
	styleWarn    = lipgloss.NewStyle().Foreground(colorWarn)
	styleBad     = lipgloss.NewStyle().Foreground(colorBad)
	styleWorking = lipgloss.NewStyle().Foreground(colorTool).Bold(true)
	styleBorder  = lipgloss.NewStyle().Foreground(colorBorder)
	styleRail    = lipgloss.NewStyle().Foreground(colorTool)
	styleGutter  = lipgloss.NewStyle().Foreground(colorThink)
	// a change marks its side with one coloured glyph. The words on the row keep the terminal's own
	// colour: a whole line of red or green is noise, and nothing on it reads.
	styleAddMark = lipgloss.NewStyle().Foreground(colorOK)
	styleDelMark = lipgloss.NewStyle().Foreground(colorBad)
	styleCode    = lipgloss.NewStyle().Foreground(colorCyan)
	styleKey     = lipgloss.NewStyle().Foreground(colorAccent)
	// A block of code marks only what a reader is looking for and leaves the rest the terminal's own
	// colour. Each keeps the source's own tabs: a style that expanded them would rewrite the code a
	// reader copies.
	styleCodeDefault = lipgloss.NewStyle().TabWidth(-1)
	styleCodeKeyword = lipgloss.NewStyle().TabWidth(-1).Foreground(colorTool).Bold(true)
	styleCodeString  = lipgloss.NewStyle().TabWidth(-1).Foreground(colorOK)
	styleCodeComment = lipgloss.NewStyle().TabWidth(-1).Foreground(colorMuted).Italic(true)
	styleCodeName    = lipgloss.NewStyle().TabWidth(-1).Foreground(colorCyan)
)

// card is text inside a rounded frame the width of the screen, a title at the left of its top edge
// and a label at the right. The body rows are expected to fit width-4 columns.
func card(title, label string, body []string, width int) []string {
	inner := width - 4
	if inner < 1 {
		return body
	}
	left := styleBorder.Render("╭─ ") + title + " "
	right := styleBorder.Render("─╮")
	if label != "" {
		right = " " + label + " " + right
	}
	fill := width - lipgloss.Width(left) - lipgloss.Width(right)
	if fill < 1 { // too narrow for the label: the title alone
		right, fill = styleBorder.Render("─╮"), width-lipgloss.Width(left)-2
	}
	rows := []string{left + styleBorder.Render(strings.Repeat("─", max(fill, 0))) + right}
	side := styleBorder.Render("│")
	for _, line := range body {
		rows = append(rows, side+" "+padTo(line, inner)+" "+side)
	}
	return append(rows, styleBorder.Render("╰"+strings.Repeat("─", width-2)+"╯"))
}

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
