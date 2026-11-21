package tui

import (
	tea "charm.land/bubbletea/v2"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/home"
)

// settingsHome puts the settings file in a scratch folder for one test.
func settingsHome(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("YKODE_HOME", dir)
	home.Reset()
	t.Cleanup(home.Reset)
	if body != "" {
		if err := os.WriteFile(filepath.Join(dir, "settings.yaml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// rowFor finds the list row that starts with a key.
func rowFor(table, key string) string {
	for _, line := range strings.Split(table, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), key) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func settingsModel(t *testing.T) *model {
	t.Helper()
	m := newTestModel()
	m.cfg = config.Config{
		BaseURL: "http://127.0.0.1:8080/v1", ThinkingLevel: "medium", MaxTurns: 25,
		ReadLines: 150, SafetyMargin: 4096,
		ExecTimeout: time.Minute, StreamIdle: 2 * time.Minute,
		KeepSessions: 720 * time.Hour, KeepCommandLogs: 24 * time.Hour,
		// as a run that read the file below resolved it: the never-ask list was set there
		Allow: []string{"exec:go test"},
	}
	return m
}

// the list shows every knob, and what someone changed stands out from what nobody chose
func TestSettingsListsEveryKnobAndWhatWasChanged(t *testing.T) {
	settingsHome(t, "# mine\nmax_turns: 30\npermissions:\n  allow:\n    - exec:go test\n")
	m := settingsModel(t)

	out := stripCodes(m.settings("/settings"))
	for _, want := range []string{"base_url", "tools.read_lines", "keep_sessions", "what this run reads"} {
		if !strings.Contains(out, want) {
			t.Errorf("the list is missing %q:\n%s", want, out)
		}
	}
	if line := rowFor(out, "max_turns"); !strings.Contains(line, "was 100 · file") {
		t.Errorf("a knob someone changed is not marked as changed: %q", line)
	}
	// the pointer is not a knob any more: the screen never asks the terminal for it
	if strings.Contains(out, "mouse") {
		t.Errorf("the list still offers the retired mouse key:\n%s", out)
	}
	if line := rowFor(out, "permissions.allow"); !strings.Contains(line, "exec:go test") {
		t.Errorf("the never-ask list is not shown: %q", line)
	}
	if line := rowFor(out, "model"); !strings.Contains(line, "unset") || strings.Contains(line, "was") {
		t.Errorf("a knob nobody set should read as unset and say nothing else: %q", line)
	}
}

// one knob explained: what it is for, what it is now, what it would be, its choices, how to change it
func TestOneKnobIsExplainedRatherThanListed(t *testing.T) {
	m := settingsModel(t)
	out := stripCodes(m.settings("/settings thinking_level"))
	for _, want := range []string{
		"how deeply the model thinks", "now medium · default medium · from default",
		"choices: off, minimal, low, medium, high, xhigh, max", "change it: /settings thinking_level VALUE",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

// a change from the screen reaches the file, and the screen says which way it moved
func TestAChangeFromTheScreenLandsInTheFile(t *testing.T) {
	dir := settingsHome(t, "# keep me\nmodel: qwen3-coder\n")
	m := settingsModel(t)

	out := stripCodes(m.settings("/settings tools.read_lines 40"))
	if !strings.Contains(out, "read_lines: 150 → 40") {
		t.Errorf("the change was not reported as a move:\n%s", out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "settings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "read_lines: 40") || !strings.Contains(body, "# keep me") || !strings.Contains(body, "model: qwen3-coder") {
		t.Errorf("the file after the change:\n%s", body)
	}
}

// a value the knob cannot take is refused and the file is left as it was
func TestABadValueIsRefusedWithoutTouchingTheFile(t *testing.T) {
	dir := settingsHome(t, "stream_idle_sec: 120\n")
	m := settingsModel(t)

	out := stripCodes(m.settings("/settings stream_idle_sec soon"))
	if !strings.Contains(out, "not changed") {
		t.Errorf("a bad value was accepted:\n%s", out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "settings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "stream_idle_sec: 120\n" {
		t.Errorf("the file was rewritten:\n%s", raw)
	}
}

// a knob nobody named gets the list of the ones there are
func TestAKnobNobodyNamedNamesTheOnesThereAre(t *testing.T) {
	settingsHome(t, "")
	m := settingsModel(t)
	out := stripCodes(m.settings("/settings mose"))
	if !strings.Contains(out, `no setting called mose`) || !strings.Contains(out, "model") {
		t.Errorf("unknown knob answered:\n%s", out)
	}
}

// the screen answers /settings itself, so it works while a turn is running
func TestSettingsIsAnsweredWhileATurnRuns(t *testing.T) {
	settingsHome(t, "")
	m := settingsModel(t)
	m.running = true
	m.input.SetValue("/settings max_turns")
	m.submit()

	out := stripCodes(m.render())
	if !strings.Contains(out, "round-trips one ask may take") {
		t.Errorf("a running turn was not allowed to answer /settings:\n%s", out)
	}
}

// modalKeys sends keys as the terminal does, running the probe command a key returns
func modalKeys(m *model, keys ...tea.KeyPressMsg) {
	for _, k := range keys {
		_, cmd := m.Update(k)
		if cmd != nil && k.Code == tea.KeyEnter {
			if msg, ok := cmd().(probeDoneMsg); ok {
				m.Update(msg)
			}
		}
	}
}

// a bare /settings opens the list of every setting over the session; esc closes it
func TestSettingsOpensTheListOfEverySetting(t *testing.T) {
	settingsHome(t, "")
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 40})
	m.input.SetValue("/settings")
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.screen == nil {
		t.Fatal("/settings did not open the screen")
	}
	view := stripCodes(m.render())
	for _, want := range []string{"Settings", "MODEL", "Server & model", "Thinking level", "Round-trips per ask",
		"PERMISSIONS", "Skip write confirmations", "esc closes"} {
		if !strings.Contains(view, want) {
			t.Errorf("the list lacks %q:\n%s", want, view)
		}
	}
	m.Update(esc)
	if m.screen != nil || strings.Contains(stripCodes(m.render()), "Server & model") {
		t.Error("esc did not give the session back")
	}
}

// every knob the catalogue has is on the list, except the three the server row stands for
func TestTheListHasEveryKnob(t *testing.T) {
	settingsHome(t, "")
	m := newTestModel()
	s := newSettingsScreen(m.cfg)
	listed := map[string]bool{}
	for _, row := range s.rows {
		listed[row.Key] = true
	}
	for _, set := range m.cfg.Catalogue() {
		if handled[set.Key] == listed[set.Key] {
			t.Errorf("%s: on the list %v, handled by the server row %v", set.Key, listed[set.Key], handled[set.Key])
		}
	}
}

// openRow moves the cursor to a knob by name and returns the session.
func openRow(t *testing.T, m *model, key string) {
	t.Helper()
	var err error
	if m.cfg, err = config.Load(nil); err != nil { // the settings a run really has, not an empty struct
		t.Fatal(err)
	}
	m.openSettings()
	for i, row := range m.screen.rows {
		if row.Key == key {
			m.screen.at = i + 1
			return
		}
	}
	t.Fatalf("%s is not on the list", key)
}

func fileHas(t *testing.T, dir, want string) bool {
	t.Helper()
	saved, _ := os.ReadFile(filepath.Join(dir, "settings.yaml"))
	return strings.Contains(string(saved), want)
}

func settingsFile(t *testing.T, dir string) string {
	t.Helper()
	saved, _ := os.ReadFile(filepath.Join(dir, "settings.yaml"))
	return string(saved)
}

// a switch turns over on enter, is written at once, and reaches the running session
func TestASwitchTurnsOverAndTheSessionFollows(t *testing.T) {
	dir := settingsHome(t, "")
	m := newTestModel()
	openRow(t, m, "assume_yes")
	m.Update(enter)
	if !fileHas(t, dir, "assume_yes: true") {
		t.Error("the switch was not written")
	}
	if !m.cfg.AssumeYes {
		t.Error("the screen still reads the old value")
	}
	if got := stripCodes(m.render()); !regexp.MustCompile(`Skip write confirmations\s+● on`).MatchString(got) {
		t.Errorf("the row does not show the new value:\n%s", got)
	}
}

// a choice steps with the arrows in place, and a live one is applied at once
func TestAChoiceStepsWithTheArrowsAndIsAppliedAtOnce(t *testing.T) {
	dir := settingsHome(t, "")
	m := newTestModel()
	openRow(t, m, "thinking_level")
	before := m.agent.ThinkingLevel()
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if m.agent.ThinkingLevel() == before || !fileHas(t, dir, "thinking_level: "+m.agent.ThinkingLevel()) {
		t.Errorf("level %q after right, file does not say it", m.agent.ThinkingLevel())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.agent.ThinkingLevel() != before {
		t.Errorf("left did not go back to %q: %q", before, m.agent.ThinkingLevel())
	}
}

// enter on a choice lists every option with the chosen one marked, and enter picks the one under the cursor
func TestAChoiceOpensAMenuOfItsOptions(t *testing.T) {
	dir := settingsHome(t, "")
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 44})
	openRow(t, m, "thinking_level")
	m.Update(enter)
	view := stripCodes(m.render())
	if !strings.Contains(view, "pick one") || !regexp.MustCompile(`● medium\s+8,192 tokens`).MatchString(view) {
		t.Fatalf("no menu with the chosen level marked:\n%s", view)
	}
	m.Update(down)
	m.Update(enter)
	if m.screen.menu != nil || m.agent.ThinkingLevel() != "high" || !fileHas(t, dir, "thinking_level: high") {
		t.Errorf("menu open %v, level %q: enter should pick high and close", m.screen.menu != nil, m.agent.ThinkingLevel())
	}
	m.Update(enter)
	m.Update(down)
	m.Update(esc)
	if m.screen.menu != nil || m.agent.ThinkingLevel() != "high" {
		t.Error("esc in the menu changed the level or left the menu open")
	}
}

// the theme menu shows each theme's colours beside its name, and says which follows the terminal
func TestTheThemeMenuShowsEachThemesColours(t *testing.T) {
	settingsHome(t, "")
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 44})
	openRow(t, m, "theme")
	m.Update(enter)
	view := stripCodes(m.render())
	if !strings.Contains(view, "follows the terminal's background") || !strings.Contains(view, "■■■■■■■  dark") {
		t.Errorf("the themes are not shown with their colours:\n%s", view)
	}
}

// a typed value is checked before the file is touched: a refusal says why, in place, and keeps the box open
func TestATypedValueIsCheckedAndARefusalStaysInPlace(t *testing.T) {
	dir := settingsHome(t, "")
	m := newTestModel()
	openRow(t, m, "max_turns")
	m.Update(enter) // opens the typing box
	if m.screen.menu == nil || m.screen.menu.kind != menuInput {
		t.Fatal("enter did not open a typing box for a count")
	}
	m.screen.menu.input.SetValue("zero")
	m.Update(enter)
	if m.screen.menu == nil || !m.screen.bad || fileHas(t, dir, "max_turns") {
		t.Errorf("open %v, refused %v, written %v: a bad value must stay in the box and out of the file",
			m.screen.menu != nil, m.screen.bad, fileHas(t, dir, "max_turns"))
	}
	m.screen.menu.input.SetValue("40")
	m.Update(enter)
	if m.screen.menu != nil || !fileHas(t, dir, "max_turns: 40") || m.cfg.MaxTurns != 40 {
		t.Errorf("a good value was not saved: open %v, turns %d", m.screen.menu != nil, m.cfg.MaxTurns)
	}
}

// esc while typing drops what was typed and writes nothing
func TestEscapeWhileTypingChangesNothing(t *testing.T) {
	dir := settingsHome(t, "")
	m := newTestModel()
	openRow(t, m, "max_turns")
	m.Update(enter)
	m.screen.menu.input.SetValue("99")
	m.Update(esc)
	if m.screen == nil || m.screen.menu != nil || fileHas(t, dir, "max_turns") {
		t.Error("esc while typing wrote the value or closed the whole screen")
	}
}

// r takes a setting back to its default, and says so when it already is one
func TestASettingIsResetToItsDefault(t *testing.T) {
	dir := settingsHome(t, "max_turns: 40\n")
	m := newTestModel()
	openRow(t, m, "max_turns")
	m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if fileHas(t, dir, "max_turns") || m.cfg.MaxTurns != 100 {
		t.Errorf("r left max_turns at %d:\n%s", m.cfg.MaxTurns, settingsFile(t, dir))
	}
	m.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if m.screen.bad || !strings.Contains(m.screen.note, "already the default") {
		t.Errorf("a second r said %q", m.screen.note)
	}
}

// the tools list is a checklist of every tool: space switches one off, and again back on
func TestTheToolsListIsAChecklistOfEveryTool(t *testing.T) {
	dir := settingsHome(t, "")
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 110, Height: 44})
	openRow(t, m, "tools.disabled")
	m.Update(enter)
	for range 3 {
		m.Update(down) // read_file, write_file, edit_file, then exec_command
	}
	m.Update(space)
	if !fileHas(t, dir, "- exec_command") {
		t.Fatalf("exec_command was not switched off:\n%s", settingsFile(t, dir))
	}
	if view := stripCodes(m.render()); !regexp.MustCompile(`\[x\] exec_command\s+switched off`).MatchString(view) ||
		!regexp.MustCompile(`\[ \] read_file\s+offered`).MatchString(view) {
		t.Errorf("the checklist does not show what is off:\n%s", view)
	}
	m.Update(space)
	if fileHas(t, dir, "exec_command") {
		t.Errorf("switching it back on left it in the file:\n%s", settingsFile(t, dir))
	}
}

// a typed list takes an entry from its last row and gives one up with d, and the next start is said
func TestATypedListAddsAndRemovesEntries(t *testing.T) {
	dir := settingsHome(t, "")
	m := newTestModel()
	openRow(t, m, "permissions.allow")
	m.Update(enter)
	m.Update(enter) // the only row is the one that adds
	m.screen.menu.input.SetValue("exec:go test")
	m.Update(enter)
	if !fileHas(t, dir, "exec:go test") {
		t.Fatal("the entry was not added")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	m.Update(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if fileHas(t, dir, "exec:go test") || len(m.cfg.Allow) != 0 {
		t.Errorf("d did not take the entry off:\n%s", settingsFile(t, dir))
	}
	m.Update(esc)
	openRow(t, m, "stream_idle_sec")
	if view := stripCodes(m.render()); !strings.Contains(view, "applies at the next start") {
		t.Errorf("a startup-only setting does not say so:\n%s", view)
	}
}

// the first row opens the server screen, and closing it returns to the list
func TestTheFirstRowOpensTheServerScreenAndComesBack(t *testing.T) {
	settingsHome(t, "")
	m := newTestModel()
	m.openSettings()
	m.Update(enter)
	if m.modal == nil {
		t.Fatal("enter on the server row did not open the server screen")
	}
	m.Update(esc)
	if m.modal != nil || m.screen == nil {
		t.Errorf("modal %v, list %v: closing the server screen should return to the list", m.modal != nil, m.screen != nil)
	}
}

// choosing a server and model switches the running session to them and saves them
func TestChoosingInTheServerScreenSwitchesTheSession(t *testing.T) {
	dir := settingsHome(t, "base_url: http://old:8080/v1\nmodel: old-model\n")
	server := &fakeServer{}
	m := newTestModel()
	m.probe = server.probe
	m.cfg.BaseURL = "http://old:8080/v1"
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.openSettings()
	m.Update(enter) // the server row

	modalKeys(m, enter, down, enter) // connect, pick the second model, accept
	if m.modal != nil {
		t.Fatalf("the server screen is still open: %s", stripCodes(m.render()))
	}
	if m.agent.Model() != "qwen-big" || m.agent.CtxSize() != 131072 {
		t.Errorf("the session runs %q with %d tokens, want qwen-big with its window", m.agent.Model(), m.agent.CtxSize())
	}
	if !fileHas(t, dir, "model: qwen-big") || !fileHas(t, dir, "base_url: http://old:8080/v1") {
		t.Error("settings.yaml does not hold the choice")
	}
	if out := stripCodes(m.log.render(100)); !strings.Contains(out, "now using qwen-big") {
		t.Errorf("the session does not say what changed:\n%s", out)
	}
	if m.screen == nil {
		t.Error("the list should be back under the server screen")
	}
}

// a turn in flight is using the client, the model and the options, so the screen waits for it
func TestSettingsWaitsForARunningTurn(t *testing.T) {
	settingsHome(t, "")
	m := newTestModel()
	m.running = true
	m.openSettings()
	if m.screen != nil {
		t.Error("the screen opened over a running turn")
	}
}

// no row is wider than the box, so a long value never wraps into a second row
func TestNoRowOfTheListWraps(t *testing.T) {
	settingsHome(t, "tools:\n  command_log_dir: /a/very/long/path/that/goes/on/and/on/and/on/and/on/and/on/and/on/and/on/and/on/and/on\n")
	m := newTestModel()
	var err error
	if m.cfg, err = config.Load(nil); err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 40})
	m.openSettings()
	rows := strings.Split(stripCodes(m.render()), "\n")
	count := 0
	for _, row := range rows {
		if strings.Contains(row, "Command log folder") {
			count++
		}
		if strings.TrimSpace(strings.Trim(row, "│ ")) == "run" {
			t.Errorf("a row wrapped its tag onto a line of its own: %q", row)
		}
	}
	if count != 1 {
		t.Errorf("command_log_dir appears on %d rows", count)
	}
}

// the thinking level shows every level's budget, the selected one marked, following the cursor
func TestTheThinkingLevelShowsItsBudgets(t *testing.T) {
	settingsHome(t, "")
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	openRow(t, m, "thinking_level")
	view := stripCodes(m.render())
	for _, want := range []string{"off · minimal 1,024", "low 2,048", "medium 8,192", "high 16,384", "xhigh 32,768", "max no cap"} {
		if !strings.Contains(view, want) {
			t.Errorf("the budgets lack %q:\n%s", want, view)
		}
	}
	if !strings.Contains(view, "[medium 8,192]") || !strings.Contains(view, "thinking stops at 8,192 tokens") {
		t.Errorf("the selected level is not marked, or its meaning is not said:\n%s", view)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight}) // medium -> high
	if view := stripCodes(m.render()); !strings.Contains(view, "[high 16,384]") || strings.Contains(view, "[medium") {
		t.Errorf("the mark did not follow the level:\n%s", view)
	}
	for i := 0; m.cfg.ThinkingLevel != "max" && i < 10; i++ {
		m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	}
	if view := stripCodes(m.render()); !strings.Contains(view, "[max no cap]") || !strings.Contains(view, "no cap on thinking") {
		t.Errorf("max does not say it has no cap:\n%s", view)
	}
}

// the box keeps its height whichever row the cursor is on, so nothing jumps as it moves
func TestTheSettingsBoxDoesNotChangeHeightWithTheCursor(t *testing.T) {
	settingsHome(t, "")
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	openRow(t, m, "thinking_level")
	with := strings.Count(stripCodes(m.render()), "\n")
	openRow(t, m, "max_turns")
	without := strings.Count(stripCodes(m.render()), "\n")
	if with != without {
		t.Errorf("the screen is %d rows on the thinking level and %d elsewhere", with, without)
	}
}

// the screen names the file it writes, and warns before touching the file every project reads
func TestTheSettingsScreenSaysWhichFileItWrites(t *testing.T) {
	settingsHome(t, "max_turns: 30\n")
	m := settingsModel(t)
	m.cfg.Root = t.TempDir()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.openSettings()

	out := stripCodes(m.render())
	if !strings.Contains(out, "⚠ global ·") || !strings.Contains(out, "/create-local-setting") {
		t.Errorf("nothing warned that the global file is the one being changed:\n%s", out)
	}

	if _, err := config.CreateLocal(m.cfg.Root, "http://127.0.0.1:8080/v1", "test-model"); err != nil {
		t.Fatal(err)
	}
	if out = stripCodes(m.render()); strings.Contains(out, "⚠ global ·") {
		t.Errorf("the warning stayed though the project has a file of its own:\n%s", out)
	}
	if !strings.Contains(out, "this project · .ykode/settings.yaml") {
		t.Errorf("the screen does not say it writes the project's file:\n%s", out)
	}
}

// /create-local-setting gives the project a file of its own and says where a change lands now
func TestCreateLocalSettingLeavesTheProjectAFile(t *testing.T) {
	settingsHome(t, "max_turns: 30\n")
	m := settingsModel(t)
	root := t.TempDir()
	// the flags this run was started with, so reading the settings again keeps the same root
	m.cfg.Root, m.cfg.Args, m.cfg.BaseURL = root, []string{"--root", root}, "http://127.0.0.1:8080/v1"

	m.input.SetValue("/create-local-setting")
	m.submit()

	raw, err := os.ReadFile(filepath.Join(root, ".ykode", "settings.yaml"))
	if err != nil {
		t.Fatalf("the project got no settings file: %v", err)
	}
	if !strings.Contains(string(raw), "base_url: http://127.0.0.1:8080/v1") {
		t.Errorf("the project file does not start from this run's server:\n%s", raw)
	}
	if notice := stripCodes(m.log.render(100)); !strings.Contains(notice, "/settings now changes this project only") {
		t.Errorf("no notice about where changes land now:\n%s", notice)
	}
}

func TestGroupDigits(t *testing.T) {
	for in, want := range map[int]string{0: "0", 999: "999", 1024: "1,024", 16384: "16,384", 1234567: "1,234,567"} {
		if got := groupDigits(in); got != want {
			t.Errorf("groupDigits(%d) = %q, want %q", in, got, want)
		}
	}
}

// a typed value belongs to the row it started on, since only that knob's kind checks it
func TestATypedValueGoesToTheRowItStartedOn(t *testing.T) {
	dir := settingsHome(t, "")
	m := newTestModel()
	openRow(t, m, "max_turns")
	m.Update(enter)
	m.screen.menu.input.SetValue("41")
	// the cursor moves to another knob of the same kind before enter is pressed
	for i, row := range m.screen.rows {
		if row.Key == "context.safety_margin" {
			m.screen.at = i + 1
			break
		}
	}
	m.Update(enter)

	if !fileHas(t, dir, "max_turns: 41") {
		t.Errorf("the value did not go to the row it was typed for:\n%s", settingsFile(t, dir))
	}
	if fileHas(t, dir, "safety_margin: 41") {
		t.Errorf("the value went to the row the cursor had reached:\n%s", settingsFile(t, dir))
	}
}

// on a window with little left the table shows what the server gets, so xhigh does not promise 32,768
func TestTheThinkingBudgetsShowTheCutTheWindowMakes(t *testing.T) {
	settingsHome(t, "")
	m := newTestModel()
	m.agent = agent.New(nil, nil, "test-model", agent.Options{CtxSize: 3000, SafetyMargin: 256}, nil)
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	openRow(t, m, "thinking_level")
	view := stripCodes(m.render())
	cut := groupDigits(m.agent.ThinkingBudget("xhigh"))
	if strings.Contains(view, "32,768") || !strings.Contains(view, "xhigh "+cut) {
		t.Errorf("xhigh should show the %s it would be sent with:\n%s", cut, view)
	}
	if !strings.Contains(view, "minimal 1,024") || !strings.Contains(view, "thinking stops at "+cut+" tokens") || !strings.Contains(view, "so they are cut to what the reply can spare") {
		t.Errorf("an uncut level changed, or the cut is not explained:\n%s", view)
	}
}
