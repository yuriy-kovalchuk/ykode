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
	// a change marks its side with one coloured glyph; the words keep the terminal's own colour
	styleAddMark = lipgloss.NewStyle().Foreground(colorOK)
	styleDelMark = lipgloss.NewStyle().Foreground(colorBad)
	styleCode    = lipgloss.NewStyle().Foreground(colorCyan)
	styleKey     = lipgloss.NewStyle().Foreground(colorAccent)
	// code marks only what a reader looks for, keeping the source's tabs so a copy is the source
	styleCodeDefault = lipgloss.NewStyle().TabWidth(-1)
	styleCodeKeyword = lipgloss.NewStyle().TabWidth(-1).Foreground(colorTool).Bold(true)
	styleCodeString  = lipgloss.NewStyle().TabWidth(-1).Foreground(colorOK)
	styleCodeComment = lipgloss.NewStyle().TabWidth(-1).Foreground(colorMuted).Italic(true)
	styleCodeName    = lipgloss.NewStyle().TabWidth(-1).Foreground(colorCyan)
)

// card is text in a rounded frame, title left, label right; body rows fit width-4 columns
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

// diffSurfaces are the four change grounds, deep or pale by the terminal, words left its own
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
