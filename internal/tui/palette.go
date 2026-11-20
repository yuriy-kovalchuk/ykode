package tui

import (
	"image/color"
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/yuriy-kovalchuk/ykode/internal/theme"
)

// colour reads a theme's colour, hex or ANSI index; "" is the terminal's own
func colour(s string) color.Color {
	if s == "" {
		return lipgloss.NoColor{}
	}
	return lipgloss.Color(s)
}

// paletteGen goes up with every switch, so rows kept from the last palette are drawn again
var paletteGen int

// the styles every drawing reads; usePalette rebuilds them all, on the goroutine that draws
var (
	styleDim, styleThink, styleBold, styleAccent, styleOK, styleWarn      lipgloss.Style
	styleBad, styleWorking, styleBorder, styleRail, styleGutter, styleKey lipgloss.Style
	// a change marks its side with one coloured glyph; the words keep the terminal's own colour
	styleAddMark, styleDelMark, styleCode lipgloss.Style
	// code marks only what a reader looks for, keeping the source's tabs so a copy is the source
	styleCodeDefault, styleCodeKeyword, styleCodeString, styleCodeComment, styleCodeName lipgloss.Style
	// the four change grounds, deep or pale by the palette, the words left their own colour
	diffSurface                                        diffSurfaces
	overlayBox, settingsBox, settingsKey, settingsLive lipgloss.Style
	// the server screen keeps the cyan it always had, now the theme's cyan
	setupBox, setupTitle, setupPick, setupError lipgloss.Style
)

// a dark background is the guess until the terminal answers, as most terminals a coder runs are
func init() { usePalette(theme.For(true)) }

// usePalette makes p the palette every style draws with from here on.
func usePalette(p theme.Palette) {
	paletteGen++
	fg := func(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(colour(c)) }
	styleDim, styleThink, styleBold = fg(p.Muted), fg(p.Think).Italic(true), lipgloss.NewStyle().Bold(true)
	styleAccent, styleOK, styleWarn, styleBad = fg(p.Accent).Bold(true), fg(p.OK), fg(p.Warn), fg(p.Bad)
	styleWorking, styleBorder, styleRail, styleGutter = fg(p.Tool).Bold(true), fg(p.Border), fg(p.Tool), fg(p.Think)
	styleAddMark, styleDelMark, styleCode, styleKey = fg(p.OK), fg(p.Bad), fg(p.Cyan), fg(p.Accent)
	code := lipgloss.NewStyle().TabWidth(-1)
	styleCodeDefault, styleCodeKeyword = code, code.Foreground(colour(p.Tool)).Bold(true)
	styleCodeString, styleCodeName = code.Foreground(colour(p.OK)), code.Foreground(colour(p.Cyan))
	styleCodeComment = code.Foreground(colour(p.Muted)).Italic(true)
	bg := func(c string) lipgloss.Style { return lipgloss.NewStyle().Background(colour(c)) }
	diffSurface = diffSurfaces{add: bg(p.AddGround), del: bg(p.DelGround), addWord: bg(p.AddWordGround), delWord: bg(p.DelWordGround)}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colour(p.Accent)).Padding(0, 2)
	overlayBox, settingsBox, settingsKey, settingsLive = box, box, fg(p.Accent).Bold(true), fg(p.OK)
	setupBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colour(p.Cyan)).Padding(1, 2)
	setupTitle, setupPick, setupError = fg(p.Cyan).Bold(true), fg(p.Cyan).Bold(true), fg(p.Bad)
}

// paletteFor is the theme a setting names; auto, or a name ykode does not have, follows the background
func paletteFor(name string, dark bool) theme.Palette {
	if p, named := theme.Named(name); named {
		return p
	}
	return theme.For(dark)
}

// paintTerminal has the terminal take a theme's own ground and text colour while the screen is up
func paintTerminal(v *tea.View, p theme.Palette) {
	if p.Background != "" {
		v.BackgroundColor = colour(p.Background)
	}
	if p.Foreground != "" {
		v.ForegroundColor = colour(p.Foreground)
	}
}

// boxStyles are the typing box's shades for p: no bar behind the cursor's row, the placeholder muted
func boxStyles(p theme.Palette) textarea.Styles {
	s := textarea.DefaultStyles(p.Dark)
	for _, state := range []*textarea.StyleState{&s.Focused, &s.Blurred} {
		state.CursorLine = lipgloss.NewStyle()
		state.Placeholder = lipgloss.NewStyle().Foreground(colour(p.Muted))
	}
	return s
}

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

// diffSurfaces are the four change grounds a diff is drawn on.
type diffSurfaces struct{ add, del, addWord, delWord lipgloss.Style }
