package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/theme"
)

// menuKind is the editor a setting opens, chosen by the kind of value it takes
type menuKind int

const (
	menuPick    menuKind = iota // one of a few choices, all listed
	menuChecks                  // a list whose entries come from a fixed set, each switched on or off
	menuEntries                 // a list of anything typed, entries added and taken off one at a time
	menuInput                   // one typed value, checked before it is written
)

// settingMenu is the editor open on one setting, drawn in the pane under the list
type settingMenu struct {
	kind   menuKind
	key    string
	cursor int
	input  textinput.Model
	typing bool // menuEntries: a new entry is being typed
}

// newSettingMenu opens the editor that suits a setting, its cursor on what is chosen now
func newSettingMenu(set config.Setting) *settingMenu {
	in := textinput.New()
	in.Prompt, in.CharLimit = "", 300
	in.SetWidth(48)
	m := &settingMenu{key: set.Key, input: in}
	switch {
	case set.List && len(set.Options) > 0:
		m.kind = menuChecks
	case set.List:
		m.kind = menuEntries
	case len(set.Options) > 0:
		m.kind, m.cursor = menuPick, max(slices.Index(set.Options, set.Value), 0)
	default:
		m.kind = menuInput
		if set.Kind != config.KindText {
			m.input.SetWidth(16) // a number or a length of time; the unit follows it
		}
		value := set.Value
		if _, err := strconv.Atoi(value); set.Kind == config.KindCount && err != nil {
			value = "" // "no cap" is how a zero reads, not something to edit
		}
		m.input.SetValue(value)
		m.input.CursorEnd()
	}
	return m
}

// focus readies the typing box for a menu that types; the others need no cursor
func (m *settingMenu) focus() tea.Cmd {
	if m.kind != menuInput && !m.typing {
		return nil
	}
	m.input.Focus()
	return textinput.Blink
}

// setting is the knob a menu edits, as the files read now, after whatever it last wrote
func (s *settingsScreen) setting(key string) config.Setting {
	for _, set := range s.rows {
		if set.Key == key {
			return set
		}
	}
	return config.Setting{Key: key}
}

// menuKey gives the open menu a press; esc closes it, and each kind takes its own keys
func (s *settingsScreen) menuKey(msg tea.KeyMsg, ed settingsEditor) tea.Cmd {
	m, set := s.menu, s.setting(s.menu.key)
	key := msg.String()
	if m.kind == menuInput || m.typing {
		return s.typingKey(msg, set, ed)
	}
	choices := m.choices(set)
	switch key {
	case "esc", "q", "ctrl+c":
		s.menu = nil
	case "up", "k":
		m.cursor = (m.cursor + len(choices) - 1) % len(choices)
	case "down", "j":
		m.cursor = (m.cursor + 1) % len(choices)
	case "home":
		m.cursor = 0
	case "end":
		m.cursor = len(choices) - 1
	case "enter", "space", " ":
		return s.choose(m, set, choices[m.cursor], ed)
	case "d", "delete", "backspace":
		if m.kind == menuEntries && m.cursor < len(set.Items) {
			if s.commit(ed.removeSetting(set.Key, set.Items[m.cursor])) {
				m.cursor = min(m.cursor, len(s.setting(set.Key).Items))
			}
		}
	}
	return nil
}

// choices are the menu's rows: the options, or a list's entries and the row that adds one
func (m *settingMenu) choices(set config.Setting) []string {
	if m.kind == menuEntries {
		return append(slices.Clone(set.Items), addEntry)
	}
	return set.Options
}

// addEntry is the last row of a typed list, where a new entry starts
const addEntry = "+ add an entry"

// choose acts on the row under a menu's cursor: picks it, switches it, or starts typing a new entry
func (s *settingsScreen) choose(m *settingMenu, set config.Setting, choice string, ed settingsEditor) tea.Cmd {
	switch m.kind {
	case menuPick:
		if s.commit(ed.writeSetting(set.Key, choice)) {
			s.menu = nil
		}
	case menuChecks:
		if slices.Contains(set.Items, choice) {
			s.commit(ed.removeSetting(set.Key, choice))
		} else {
			s.commit(ed.writeSetting(set.Key, choice))
		}
	case menuEntries:
		if choice == addEntry {
			m.typing = true
			m.input.SetValue("")
			return m.focus()
		}
	}
	return nil
}

// typingKey edits a typed value: enter writes it, checked first, and esc leaves the file as it was
func (s *settingsScreen) typingKey(msg tea.KeyMsg, set config.Setting, ed settingsEditor) tea.Cmd {
	m := s.menu
	switch msg.String() {
	case "esc", "ctrl+c":
		if m.typing {
			m.typing = false
			m.input.Blur()
			return nil
		}
		s.menu = nil
		return nil
	case "enter":
		value := strings.TrimSpace(m.input.Value())
		if m.typing {
			if value != "" && s.commit(ed.writeSetting(set.Key, value)) {
				m.typing, m.cursor = false, len(s.setting(set.Key).Items)
				m.input.Blur()
			}
			return nil
		}
		if value == "" && set.From == config.FromDefault {
			s.menu = nil // nothing typed over a default leaves the default
			return nil
		}
		if s.commit(ed.writeSetting(set.Key, value)) {
			s.menu = nil
		}
		return nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return cmd
}

// paste types into the value being edited; with no typing box open there is nowhere for it to go
func (s *settingsScreen) paste(msg tea.PasteMsg) tea.Cmd {
	if s.menu == nil || (s.menu.kind != menuInput && !s.menu.typing) {
		return nil
	}
	var cmd tea.Cmd
	s.menu.input, cmd = s.menu.input.Update(msg)
	return cmd
}

// view draws the menu in the pane: a title naming what it does, then its rows around the cursor
func (m *settingMenu) view(width int, s *settingsScreen) []string {
	set := s.setting(m.key)
	title := settingsKey.Render(set.Label)
	switch m.kind {
	case menuInput:
		field := "› " + m.input.View()
		if set.Unit != "" {
			field += "  " + setupDim.Render(set.Unit)
		}
		lines := []string{title + setupDim.Render("  · type a value"), "", field, ""}
		for _, l := range append(wrap(set.Note, width), wrap(inputHelp(set), width)...) {
			lines = append(lines, setupDim.Render(l))
		}
		return lines
	case menuChecks:
		title += setupDim.Render("  · switch each one on or off")
	case menuEntries:
		title += setupDim.Render("  · add an entry, or take one off")
	default:
		title += setupDim.Render("  · pick one")
	}
	choices := m.choices(set)
	room := paneRows - 2
	first := min(max(m.cursor-room/2, 0), max(len(choices)-room, 0))
	lines := []string{title, ""}
	for i := first; i < min(first+room, len(choices)); i++ {
		lines = append(lines, m.choiceRow(i, choices[i], set, width, s))
	}
	return lines
}

// choiceRow is one row of a menu: the cursor, a mark for what is chosen, the choice, and what it means
func (m *settingMenu) choiceRow(i int, choice string, set config.Setting, width int, s *settingsScreen) string {
	cursor := "  "
	if i == m.cursor {
		cursor = setupPick.Render("❯ ")
	}
	var mark, about string
	switch m.kind {
	case menuPick:
		mark, about = "○ ", choiceAbout(set.Key, choice, s)
		if choice == set.Value {
			mark = styleOK.Render("● ")
		}
	case menuChecks:
		mark, about = "[ ] ", setupDim.Render("offered")
		if slices.Contains(set.Items, choice) {
			mark, about = styleWarn.Render("[x] "), styleWarn.Render("switched off")
		}
	case menuEntries:
		if choice == addEntry {
			if m.typing {
				return cursor + "› " + m.input.View()
			}
			return cursor + styleAccent.Render(choice)
		}
	}
	name := choice
	if i == m.cursor {
		name = settingsKey.Render(choice)
	}
	row := cursor + mark + name
	if about != "" {
		pad := max(18-lipgloss.Width(choice), 1)
		row += strings.Repeat(" ", pad) + about
	}
	return cutToWidth(row, width)
}

// choiceAbout is what a choice means, where the screen can say: off's lack of thinking, a theme's colours
func choiceAbout(key, choice string, s *settingsScreen) string {
	switch key {
	case "thinking_level":
		if choice != llm.ThinkingOff {
			return "" // the others are sent as the word they spell, which the row already shows
		}
		return setupDim.Render("no thinking")
	case "theme":
		palette, named := theme.Named(choice)
		if !named {
			return setupDim.Render("follows the terminal's background")
		}
		return swatch(palette)
	}
	return ""
}

// swatch is a theme's colours side by side, so a choice is seen before it is made
func swatch(p theme.Palette) string {
	var b strings.Builder
	for _, hex := range []string{p.Accent, p.Cyan, p.OK, p.Warn, p.Bad, p.Tool, p.Muted} {
		b.WriteString(lipgloss.NewStyle().Foreground(colour(hex)).Render("■"))
	}
	shade := "light"
	if p.Dark {
		shade = "dark"
	}
	return b.String() + setupDim.Render("  "+shade)
}

// inputHelp says what a typed value must be, and what an empty one does
func inputHelp(set config.Setting) string {
	what := map[string]string{
		config.KindCount:    "a whole number",
		config.KindSeconds:  "a whole number of seconds above zero",
		config.KindDuration: "a length of time, like 720h or 90m",
	}[set.Kind]
	if what == "" {
		what = "any text"
	}
	return fmt.Sprintf("%s · empty takes it back to the default of %s", what,
		shownValue(config.Setting{Value: set.Default, Unit: set.Unit}))
}

// hint is the keys the open menu takes.
func (m *settingMenu) hint() string {
	switch {
	case m.kind == menuInput:
		return "enter saves · esc cancels"
	case m.typing:
		return "enter adds the entry · esc cancels"
	case m.kind == menuChecks:
		return "space or enter switches it · ↑/↓ moves · esc done"
	case m.kind == menuEntries:
		return "enter on + adds · d removes the entry · ↑/↓ moves · esc done"
	}
	return "enter picks · ↑/↓ moves · esc cancels"
}
