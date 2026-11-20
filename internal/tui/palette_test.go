package tui

import (
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/theme"
)

// codeOf is the escape a style opens its text with.
func codeOf(style lipgloss.Style) string {
	open, _, _ := strings.Cut(style.Render("x"), "x")
	return open
}

// stripCodes reads a row without styles; a code ends at its final byte, not at the next "m"
func stripCodes(s string) string {
	for {
		i := strings.Index(s, "\x1b[")
		if i < 0 {
			return s
		}
		j := i + 2
		for j < len(s) && (s[j] < '@' || s[j] > '~') {
			j++
		}
		if j >= len(s) {
			return s[:i]
		}
		s = s[:i] + s[j+1:]
	}
}

// a light terminal gets the light palette, and rows drawn before it answered are drawn again in it
func TestALightBackgroundRedrawsTheSessionInTheLightPalette(t *testing.T) {
	t.Cleanup(func() { usePalette(theme.For(true)) })
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(eventMsg{ev: agent.Event{Kind: agent.Answer, Text: "```go\nfunc main() {}\n```\n"}})
	m.Update(runDoneMsg{})
	dark := codeOf(styleCodeKeyword)
	if !strings.Contains(m.render(), dark) {
		t.Fatal("the keyword is not drawn in the dark palette to begin with")
	}
	m.Update(tea.BackgroundColorMsg{Color: color.White})
	light := codeOf(styleCodeKeyword)
	if light == dark {
		t.Fatal("the light palette colours a keyword as the dark one does")
	}
	if out := m.render(); !strings.Contains(out, light) || strings.Contains(out, dark) {
		t.Errorf("the session kept rows from the dark palette:\n%q", out)
	}
}

// a dark terminal answering changes nothing that was drawn
func TestADarkBackgroundKeepsTheDarkPalette(t *testing.T) {
	m := newTestModel()
	m.Update(tea.BackgroundColorMsg{Color: color.Black})
	if codeOf(styleCodeKeyword) != codeOf(lipgloss.NewStyle().Foreground(colour(theme.For(true).Tool)).Bold(true)) {
		t.Error("a dark background did not keep the dark palette")
	}
}

// themedModel is a session started with the theme setting at name, its palette put back after the test
func themedModel(t *testing.T, name string) *model {
	t.Helper()
	t.Cleanup(func() { usePalette(theme.For(true)) })
	a := agent.New(nil, nil, "test-model", agent.Options{}, nil)
	m := newModel(t.Context(), a, NewHub(), config.Config{Theme: name})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}

// a named theme colours the screen from the first frame and paints the terminal's ground and text
func TestANamedThemeColoursTheScreenAndPaintsTheTerminal(t *testing.T) {
	m := themedModel(t, "dracula")
	dracula, _ := theme.Named("dracula")
	if codeOf(styleCodeKeyword) != codeOf(lipgloss.NewStyle().Foreground(colour(dracula.Tool)).Bold(true)) {
		t.Error("a keyword is not drawn in dracula's colour")
	}
	v := m.View()
	if v.BackgroundColor == nil || v.ForegroundColor == nil {
		t.Fatal("the terminal is left its own ground under a theme that has one")
	}
	if r, g, b, _ := v.BackgroundColor.RGBA(); r>>8 != 0x28 || g>>8 != 0x2a || b>>8 != 0x36 {
		t.Errorf("the ground is %v, want dracula's #282a36", v.BackgroundColor)
	}
}

// a named theme stays when the terminal reports its background; only auto follows the report
func TestANamedThemeIgnoresTheTerminalsBackground(t *testing.T) {
	m := themedModel(t, "solarized-light")
	before := codeOf(styleCodeKeyword)
	m.Update(tea.BackgroundColorMsg{Color: color.Black})
	if codeOf(styleCodeKeyword) != before {
		t.Error("a dark terminal swapped a named light theme for ykode's dark one")
	}
}

// auto, and the themes drawn on the terminal's own ground, leave the terminal's colours alone
func TestAThemeWithoutAGroundLeavesTheTerminalAlone(t *testing.T) {
	for _, name := range []string{theme.Auto, "ykode-dark", "ykode-light", "terminal"} {
		v := themedModel(t, name).View()
		if v.BackgroundColor != nil || v.ForegroundColor != nil {
			t.Errorf("%s repainted the terminal", name)
		}
	}
}

// choosing a theme on the settings screen saves it and redraws the session in it at once
func TestAThemeChosenInSettingsRedrawsTheSession(t *testing.T) {
	settingsHome(t, "")
	m := themedModel(t, theme.Auto)
	m.Update(eventMsg{ev: agent.Event{Kind: agent.Answer, Text: "```go\nfunc main() {}\n```\n"}})
	m.Update(runDoneMsg{})
	var err error
	if m.cfg, err = config.Load(nil); err != nil {
		t.Fatal(err)
	}
	m.openSettings()
	if err := m.writeSetting("theme", "gruvbox-dark"); err != nil {
		t.Fatal(err)
	}
	gruvbox, _ := theme.Named("gruvbox-dark")
	want := codeOf(lipgloss.NewStyle().Foreground(colour(gruvbox.Tool)).Bold(true))
	m.screen = nil
	if out := m.render(); !strings.Contains(out, want) {
		t.Errorf("the session was not redrawn in gruvbox:\n%q", out)
	}
	if m.View().BackgroundColor == nil {
		t.Error("the terminal kept its ground after a theme with one was chosen")
	}
}
