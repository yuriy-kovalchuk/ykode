package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// settingsScreen is /settings: every knob grouped, each changed through the menu that suits its kind
type settingsScreen struct {
	rows   []config.Setting       // the knobs after the server row, in the order their groups are shown
	at     int                    // the row under the cursor; 0 is the server row
	menu   *settingMenu           // the editor open on the row under the cursor; nil while browsing
	note   string                 // what the last change did, or why it did not happen
	bad    bool                   // the note is a refusal
	budget func(level string) int // what a level gets after the window's cut; nil shows the full table
}

// settingsEditor is what the screen changes settings through: the session writes, reloads and applies
type settingsEditor interface {
	writeSetting(key, value string) error
	removeSetting(key, value string) error
}

// paneRows is what the detail pane always takes, so the box keeps its height: a menu needs most
const paneRows = 9

// handled are the knobs the server row stands for: they are not listed twice.
var handled = map[string]bool{"base_url": true, "model": true}

// liveKnobs are the settings a running session takes up at once, from config's one key list
var liveKnobs = config.LiveKnobs()

// groupOrder is the order the headings are shown in; the server row heads the first
var groupOrder = []string{config.GroupModel, config.GroupContext, config.GroupTools, config.GroupPermissions,
	config.GroupSessions, config.GroupAppearance}

func newSettingsScreen(cfg config.Config) *settingsScreen {
	s := &settingsScreen{}
	s.load(cfg)
	return s
}

// load reads the resolved settings again, grouped, for the rows and for the value shown beside each
func (s *settingsScreen) load(cfg config.Config) {
	s.rows = s.rows[:0]
	all := cfg.Catalogue()
	for _, group := range groupOrder {
		for _, set := range all {
			if set.Group == group && !handled[set.Key] {
				s.rows = append(s.rows, set)
			}
		}
	}
}

// current is the knob under the cursor; the server row has none.
func (s *settingsScreen) current() (config.Setting, bool) {
	if s.at == 0 {
		return config.Setting{}, false
	}
	return s.rows[s.at-1], true
}

type settingsAction int

const (
	actNone settingsAction = iota
	actClose
	actServer
)

// key handles a press: an open menu has it first, then the list moves, opens a menu or resets
func (s *settingsScreen) key(msg tea.KeyMsg, ed settingsEditor) (settingsAction, tea.Cmd) {
	if s.menu != nil {
		return actNone, s.menuKey(msg, ed)
	}
	set, onKnob := s.current()
	switch msg.String() {
	case "esc", "q", "ctrl+c":
		return actClose, nil
	case "up", "k":
		s.at = (s.at + len(s.rows)) % (len(s.rows) + 1)
	case "down", "j":
		s.at = (s.at + 1) % (len(s.rows) + 1)
	case "pgup":
		s.at = max(s.at-8, 0)
	case "pgdown":
		s.at = min(s.at+8, len(s.rows))
	case "home":
		s.at = 0
	case "end":
		s.at = len(s.rows)
	case "left", "h", "right", "l":
		// a choice steps through its options in place, the quick way; its menu shows them all
		if onKnob && len(set.Options) > 0 && !set.List {
			by := 1
			if k := msg.String(); k == "left" || k == "h" {
				by = -1
			}
			s.step(set, by, ed)
		}
	case "r", "backspace", "delete":
		if onKnob {
			s.reset(set, ed)
		}
	case "enter", "space", " ":
		switch {
		case !onKnob:
			return actServer, nil
		case set.Switch:
			next := "on"
			if set.Value == "on" {
				next = "off"
			}
			s.commit(ed.writeSetting(set.Key, next))
		default:
			s.note, s.bad = "", false
			s.menu = newSettingMenu(set)
			return actNone, s.menu.focus()
		}
	}
	return actNone, nil
}

// step moves a choice to the next or previous of its options and writes it.
func (s *settingsScreen) step(set config.Setting, by int, ed settingsEditor) {
	at, n := slices.Index(set.Options, set.Value), len(set.Options)
	s.commit(ed.writeSetting(set.Key, set.Options[((at+by)%n+n)%n]))
}

// reset takes a knob back to its built-in, which only a file that chose something can do
func (s *settingsScreen) reset(set config.Setting, ed settingsEditor) {
	if set.From == config.FromDefault {
		s.note, s.bad = set.Label+" is already the default", false
		return
	}
	s.commit(ed.writeSetting(set.Key, "unset"))
}

// commit reports what a write did: a refusal says why and leaves the file alone
func (s *settingsScreen) commit(err error) bool {
	if err != nil {
		s.note, s.bad = strings.TrimPrefix(err.Error(), "not changed: "), true
		return false
	}
	s.bad = false
	return true
}

// listLine is one line of the scrolled list: a group's heading, or a row by its index
type listLine struct {
	heading string
	row     int
}

// lines are the headings and the rows, the server row under the first heading
func (s *settingsScreen) lines() []listLine {
	out := []listLine{{heading: groupOrder[0]}, {row: 0}}
	last := groupOrder[0]
	for i, set := range s.rows {
		if set.Group != last {
			out = append(out, listLine{heading: set.Group})
			last = set.Group
		}
		out = append(out, listLine{row: i + 1})
	}
	return out
}

// view draws the screen within width and height columns and rows of terminal.
func (s *settingsScreen) view(width, height int, cfg config.Config, model string) string {
	inner := min(max(width-8, 50), 100)
	text := inner - 4 // the box's border and padding
	head := s.header(cfg, text)
	// the head, a blank, the list, a rule, the pane, the note, a rule, the hint, and the box's edges
	room := max(height-len(head)-1-1-paneRows-1-1-1-4, 4)
	all := s.lines()
	cursor := slices.IndexFunc(all, func(l listLine) bool { return l.heading == "" && l.row == s.at })
	first := min(max(cursor-room/2, 0), max(len(all)-room, 0))
	if all[max(cursor-1, 0)].heading != "" && first == cursor {
		first = cursor - 1 // a row at the top of the window keeps the heading it sits under
	}

	labelW := len("Server & model")
	for _, set := range s.rows {
		labelW = max(labelW, lipgloss.Width(set.Label))
	}
	rule := styleBorder.Render(strings.Repeat("─", text))
	out := append(head, "")
	for _, l := range all[first:min(first+room, len(all))] {
		if l.heading != "" {
			out = append(out, styleDim.Render(strings.ToUpper(l.heading)))
			continue
		}
		out = append(out, s.row(l.row, labelW, text, cfg, model))
	}
	out = append(out, rule)
	out = append(out, s.pane(text)...)
	out = append(out, s.noteLine(text), rule, setupDim.Render(cutToWidth(s.hint(), text)))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
		settingsBox.Width(framed(settingsBox, inner)).Render(strings.Join(out, "\n")))
}

// header is the title, and which file a change lands in, with a warning when every project reads it
func (s *settingsScreen) header(cfg config.Config, width int) []string {
	where, local := config.SettingsTarget(cfg.Root)
	scope := setupDim.Render("this project · .ykode/settings.yaml")
	if !local {
		scope = styleWarn.Render("⚠ global · every project reads it")
	}
	title := setupTitle.Render("Settings")
	gap := max(width-lipgloss.Width(title)-lipgloss.Width(scope), 2)
	head := []string{title + strings.Repeat(" ", gap) + scope}
	if !local {
		head = append(head, setupDim.Render(cutToWidth(shortenHome(where), width)),
			setupDim.Render(cutToWidth("/create-local-setting gives this project a file of its own", width)))
	}
	return head
}

// row is one knob: the cursor, its label, its value, and whether a running session takes it at once
func (s *settingsScreen) row(i, labelW, width int, cfg config.Config, model string) string {
	mark := "  "
	if i == s.at {
		mark = setupPick.Render("❯ ")
	}
	if i == 0 {
		what := cutToWidth(fmt.Sprintf("%s · %s", orUnset(model), cfg.BaseURL), max(width-2-labelW-2, 10))
		return mark + settingsLabel("Server & model", labelW, i == s.at) + "  " + what
	}
	set := s.rows[i-1]
	tag := setupDim.Render("restart")
	if set.Live {
		tag = settingsLive.Render("   live")
	}
	room := max(width-2-labelW-2-2-lipgloss.Width(tag), 8)
	value := cutToWidth(shownValue(set), room)
	gap := strings.Repeat(" ", max(room-lipgloss.Width(value), 0)+2)
	switch {
	case set.Switch && set.Value == "on":
		value = styleOK.Render(value)
	case set.From == config.FromDefault:
		value = styleDim.Render(value)
	}
	return mark + settingsLabel(set.Label, labelW, i == s.at) + "  " + value + gap + tag
}

// settingsLabel pads a label to its column, bright under the cursor
func settingsLabel(label string, width int, picked bool) string {
	padded := label + strings.Repeat(" ", max(width-lipgloss.Width(label), 0))
	if picked {
		return settingsKey.Render(padded)
	}
	return padded
}

// shownValue is a knob's value as a reader takes it in: units said, switches marked, empty lists named
func shownValue(set config.Setting) string {
	switch {
	case set.Switch && set.Value == "on":
		return "● on"
	case set.Switch:
		return "○ off"
	case set.List && set.Value == "":
		return "none"
	case set.Value == "":
		return "default"
	}
	if n, err := strconv.Atoi(set.Value); err == nil && set.Unit != "" {
		return groupDigits(n) + " " + set.Unit
	}
	return set.Value
}

// pane is the knob under the cursor explained, or the menu open on it, always paneRows tall
func (s *settingsScreen) pane(width int) []string {
	var lines []string
	if s.menu != nil {
		lines = s.menu.view(width, s)
	} else {
		lines = s.detail(width)
	}
	for len(lines) < paneRows {
		lines = append(lines, "")
	}
	return lines[:paneRows]
}

// detail explains the knob under the cursor: what it is, where its value came from, and when it applies
func (s *settingsScreen) detail(width int) []string {
	set, ok := s.current()
	if !ok {
		return append([]string{settingsKey.Render("Server & model")},
			wrap("the server ykode asks and the model it runs: enter connects, lists the models and switches to the one picked", width)...)
	}
	lines := []string{settingsKey.Render(set.Label) + "  " + setupDim.Render(set.Key)}
	lines = append(lines, wrap(set.Note, width)...)
	when := "applies at the next start"
	if set.Live {
		when = "applies now"
	}
	def := shownValue(config.Setting{Value: set.Default, Unit: set.Unit, Switch: set.Switch, List: set.List})
	lines = append(lines, setupDim.Render(cutToWidth(fmt.Sprintf("default %s · %s · %s", def, fromWords(set.From), when), width)))
	if set.From == config.FromEnv || set.From == config.FromFlag {
		lines = append(lines, styleWarn.Render(cutToWidth("a "+set.From+" sets this for the run: the file is changed, the "+
			set.From+" still wins", width)))
	}
	if set.Key == "thinking_level" {
		// the budget table is one styled row cut to the box: a break would leak its colour
		lines = append(lines, "", cutToWidth(s.budgetLine(set.Value), width))
		for _, row := range wrap(s.budgetNote(set.Value), width) {
			lines = append(lines, setupDim.Render(row))
		}
	}
	return lines
}

// fromWords says where a value came from as a sentence would.
func fromWords(from string) string {
	switch from {
	case config.FromDefault:
		return "not set"
	case config.FromFile:
		return "set in the global file"
	case config.FromProject:
		return "set in this project's file"
	}
	return "set by " + from
}

// noteLine is what the last change did, green, or why it was refused, red; blank when nothing happened
func (s *settingsScreen) noteLine(width int) string {
	if s.note == "" {
		return ""
	}
	style := styleOK
	if s.bad {
		style = styleBad
	}
	return style.Render(cutToWidth(s.note, width))
}

func (s *settingsScreen) hint() string {
	if s.menu != nil {
		return s.menu.hint()
	}
	set, ok := s.current()
	switch {
	case !ok:
		return "enter opens the server screen · ↑/↓ moves · esc closes"
	case set.Switch:
		return "enter turns it on or off · r resets · ↑/↓ moves · esc closes"
	case len(set.Options) > 0 && !set.List:
		return "enter lists the choices · ←/→ steps through them · r resets · ↑/↓ moves · esc closes"
	case set.List:
		return "enter edits the list · r empties it · ↑/↓ moves · esc closes"
	}
	return "enter edits the value · r resets · ↑/↓ moves · esc closes"
}

// ---- the session's side of the screen ------------------------------------------------------------

// openSettings waits for the turn: a running request is using the client, model and options
func (m *model) openSettings() {
	if m.running {
		m.log.notice("settings change between turns: press esc to stop this one first")
		return
	}
	m.screen = newSettingsScreen(m.cfg)
	m.screen.budget = m.agent.ThinkingBudget // the table says what the server gets, not what the level asks
}

// openServer is the first row: the screen a first run starts with, over this one.
func (m *model) openServer() {
	m.modal = newSetup(SetupStart{BaseURL: m.cfg.BaseURL, Model: m.agent.Model(), Inside: true}, m.probe)
	m.modal.width = m.width
}

// screenMsg gives the settings screen its keys. The server screen, when it is open, has them first.
func (m *model) screenMsg(msg tea.Msg) (tea.Cmd, bool) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil, false
	}
	action, cmd := m.screen.key(key, m)
	switch action {
	case actClose:
		m.screen = nil
		m.refresh()
	case actServer:
		m.openServer()
	}
	return cmd, true
}

// writeSetting writes a knob, reloads the settings, and applies what a live session can take
func (m *model) writeSetting(key, value string) error {
	before := m.cfg
	_, local, err := config.Set(m.cfg.Root, key, value)
	if err != nil {
		return err
	}
	return m.reloadSettings(before, key, m.screenSaved(key, local))
}

// removeSetting takes one entry off a list setting, then reloads as any other change does
func (m *model) removeSetting(key, value string) error {
	before := m.cfg
	_, local, err := config.Remove(m.cfg.Root, key, value)
	if err != nil {
		return err
	}
	return m.reloadSettings(before, key, m.screenSaved(key, local))
}

// screenSaved says a change was written, by the name the screen shows it under and the file it went to
func (m *model) screenSaved(key string, local bool) string {
	label := key
	if set, ok := find(m.cfg.Catalogue(), key); ok {
		label = set.Label
	}
	if local {
		return "✓ " + label + " saved to this project's settings"
	}
	return "✓ " + label + " saved to the global settings"
}

// reloadSettings reads the files back after a change, so the screen shows what the next run will read
func (m *model) reloadSettings(before config.Config, key, said string) error {
	fresh, err := m.cfg.Reload()
	if err != nil {
		return err
	}
	m.cfg = fresh
	m.screen.load(fresh)
	m.screen.note = said
	if liveKnobs[key] {
		m.applyLive(before, fresh)
	}
	return nil
}

// applyLive hands the session what the change moved. Only the knobs in liveKnobs are here.
func (m *model) applyLive(before, now config.Config) {
	if now.Theme != before.Theme {
		m.useTheme(now.Theme) // the settings screen itself is redrawn in it as the choice lands
	}
	if now.ThinkingLevel != before.ThinkingLevel {
		// only a change to the level is written: saving the old value would undo an unsaved /think
		m.agent.SetThinkingLevel(now.ThinkingLevel) // a turn in flight refuses, and says so itself
	}
	m.agent.Tune(func(o *agent.Options) {
		o.AssumeYes = now.AssumeYes
		o.MaxTurns, o.MaxTokens, o.SafetyMargin = now.MaxTurns, now.MaxTokens, now.SafetyMargin
		// the model cards travel with the run, so a later /model takes the model it switches to
		o.Cards = now.Cards()
		if card, known := o.Cards[m.agent.Model()]; known {
			o.Thinking, o.NoThink = card.Thinking, card.NoThink
		}
	})
}

// budgetLine is every level with its token budget, the selected one marked, as the server gets them
func (s *settingsScreen) budgetLine(selected string) string {
	parts := make([]string, 0, 7)
	for _, level := range llm.ThinkingLevels() {
		text := strings.TrimSpace(level + " " + budgetText(level, s.budgetOf(level)))
		if level == selected {
			text = settingsKey.Render("[" + text + "]")
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, " · ")
}

// budgetOf is what a level is sent with: its full budget, or less when the window has less to give
func (s *settingsScreen) budgetOf(level string) int {
	if s.budget == nil {
		budget, _ := llm.ThinkingBudgetTokens(level)
		return budget
	}
	return s.budget(level)
}

// cut says whether the window holds any level below its full budget right now
func (s *settingsScreen) cut() bool {
	for _, level := range llm.ThinkingLevels() {
		if full, _ := llm.ThinkingBudgetTokens(level); full > 0 && s.budgetOf(level) < full {
			return true
		}
	}
	return false
}

// budgetText is a level's thinking budget in words; off has none to give and max has no cap.
func budgetText(level string, budget int) string {
	switch {
	case level == llm.ThinkingOff:
		return "" // nothing to think for: the level's name says it
	case budget <= 0:
		return "no cap"
	}
	return groupDigits(budget)
}

// budgetNote says what the selected level's budget does to the reply, and why it is less when it is
func (s *settingsScreen) budgetNote(level string) string {
	note := ""
	switch budget := s.budgetOf(level); {
	case level == llm.ThinkingOff:
		note = "the model answers straight away, with no thinking phase"
	case budget <= 0:
		note = "no cap on thinking: it shares the whole reply with the answer, so a long thought can leave none"
	default:
		note = fmt.Sprintf("thinking stops at %s tokens and the answer takes the rest of the reply", groupDigits(budget))
	}
	if s.cut() {
		note += "; the window has too little left for the full budgets, so they are cut to what the reply can spare " +
			"(/compact or /reset gives the room back)"
	}
	return note
}

// groupDigits writes 16384 as 16,384.
func groupDigits(n int) string {
	text := fmt.Sprint(n)
	for i := len(text) - 3; i > 0; i -= 3 {
		text = text[:i] + "," + text[i:]
	}
	return text
}

// openSettingsOn opens with the cursor on one setting, rather than printing its value
func (m *model) openSettingsOn(key string) {
	m.openSettings()
	if m.screen == nil {
		return
	}
	for i, set := range m.screen.rows {
		if set.Key == key {
			m.screen.at = i + 1 // the first row is the server and model, above the settings themselves
			return
		}
	}
}
