package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/audit"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
)

// historyModel is a session that has made two calls, one that read a file and one that failed, plus any the
// test adds.
func historyModel(t *testing.T, extra ...audit.Entry) *model {
	t.Helper()
	dir, conversation := t.TempDir(), "s20260101-120000.000"
	log := audit.New(dir, conversation)
	for _, e := range []audit.Entry{
		{ID: "r1", Tool: "read_file", Args: `{"path":"main.go"}`, Paths: []string{filepath.Join(dir, "main.go")},
			Status: "ok", Decision: "auto", DurationMS: 12},
		{ID: "r2", Tool: "exec_command", Args: `{"command":"go test ./internal/tui/ -run 'History' -v 2>&1 | grep -E '^--- FAIL' | head -20","timeout":240}`, Status: "error", Decision: "user_yes", DurationMS: 230},
	} {
		if err := log.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range extra {
		e.Session, e.Turn, e.Root, e.Model = conversation, 1, dir, "test-model"
		if err := log.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	a := agent.New(nil, nil, "test-model", agent.Options{Audit: log, Root: dir}, nil)
	m := newModel(context.Background(), a, NewHub(), config.Config{Root: dir, Session: conversation})
	m.Update(tea.WindowSizeMsg{Width: 92, Height: 24})
	return m
}

// openHistory is /history with whatever the command was given.
func openHistory(t *testing.T, m *model, line string) string {
	t.Helper()
	m.input.SetValue(line)
	m.submit()
	return stripCodes(m.View())
}

// /history lists every call the run made, newest first, telling an ended-wrong one from the rest
func TestHistoryListsTheCallsOfTheRun(t *testing.T) {
	m := historyModel(t)
	view := openHistory(t, m, "/history")

	if m.picker == nil {
		t.Fatal("/history did not open the list")
	}
	for _, want := range []string{"ykode · history", "2 calls this run logged", "r1", "r2", "main.go", "go test"} {
		if !strings.Contains(view, want) {
			t.Errorf("the list lacks %q:\n%s", want, view)
		}
	}
	if strings.Index(view, "r2") > strings.Index(view, "r1") {
		t.Error("the call that happened last is not listed first")
	}
	if row := rowHolding(view, "r2"); !strings.Contains(row, "✗") {
		t.Errorf("the call that ended wrong is not told apart: %q", row)
	}
}

// the words the command takes narrow the list, as they narrowed the table
func TestHistoryNarrowsToWhatTheCommandAskedFor(t *testing.T) {
	m := historyModel(t)
	view := openHistory(t, m, "/history failed")

	if !strings.Contains(view, "r2") || strings.Contains(view, "r1 ") {
		t.Errorf("a narrowed list still holds the call that worked:\n%s", view)
	}
}

// enter reads the call under the cursor inside the box: a call is not written into the conversation, where
// the model would read it back and a diff would scroll away
func TestEnterReadsTheCallInsideTheBox(t *testing.T) {
	m := historyModel(t)
	openHistory(t, m, "/history")
	m.Update(enter)

	if m.picker == nil {
		t.Fatal("reading a call closed the box instead of opening the call in it")
	}
	view := stripCodes(m.View())
	for _, want := range []string{"asked for:", "command: go test ./internal/tui/", "timeout: 240", "r2 exec_command", "allowed by user_yes", "error"} {
		if !strings.Contains(view, want) {
			t.Errorf("the call in the box lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, `{"command"`) {
		t.Errorf("the call is shown as the JSON the model wrote:\n%s", view)
	}
	if out := m.log.render(92); strings.Contains(out, "asked for:") {
		t.Errorf("the call was written into the conversation:\n%s", out)
	}
}

// a long command is read over as many rows as it needs, which is what the box has room for once the list
// steps back to the one row it is about
func TestALongCommandIsReadWholeInsideTheBox(t *testing.T) {
	m := historyModel(t)
	openHistory(t, m, "/history")
	m.Update(enter)
	view := stripCodes(m.View())

	if !strings.Contains(view, "head -20") {
		t.Errorf("the command lost its end:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > m.width {
			t.Errorf("a row runs past the terminal (%d columns): %q", lipgloss.Width(line), line)
		}
	}
}

// esc leaves the call and goes back to the list; a second esc closes the box
func TestEscLeavesTheCallForTheListAndThenTheSession(t *testing.T) {
	m := historyModel(t)
	openHistory(t, m, "/history")
	m.Update(enter)

	m.Update(esc)
	if m.picker == nil {
		t.Fatal("esc from a call closed the box; it should go back to the list first")
	}
	if s, ok := m.picker.(*historyScreen); ok && s.expanded {
		t.Error("esc left the call open")
	}
	m.Update(esc)
	if m.picker != nil {
		t.Error("esc from the list did not give the session back")
	}
}

// the side arrows step to the call next to the one being read, which is how a run's trouble is read: one
// call after another without going back through the list
func TestTheSideArrowsStepBetweenCallsWhileOneIsRead(t *testing.T) {
	m := historyModel(t)
	openHistory(t, m, "/history")
	m.Update(enter) // r2, the one that ended wrong
	m.Update(right)

	s, ok := m.picker.(*historyScreen)
	if !ok || !s.expanded {
		t.Fatal("stepping to another call dropped out of reading it")
	}
	if view := stripCodes(m.View()); !strings.Contains(view, "r1 read_file") || !strings.Contains(view, "path: main.go") {
		t.Errorf("the side arrow did not bring up the other call:\n%s", view)
	}
}

// a call longer than the box is scrolled through it, and the box says how much of it is still below: what
// the model asked for is never left out to make the box fit
func TestALongCallIsScrolledThroughRatherThanCut(t *testing.T) {
	body := "line 1 of what the call wrote\n" + strings.Repeat("a line of it\n", 40) + "the last line of what the call wrote"
	m := historyModel(t, audit.Entry{ID: "r9", Tool: "write_file", Status: "ok", Decision: "auto",
		Paths: []string{filepath.Join(t.TempDir(), "big.go")},
		Args:  `{"path":"big.go","content":"` + body + `"}`})
	openHistory(t, m, "/history r9")

	view := stripCodes(m.View())
	if !strings.Contains(view, "line 1 of what the call wrote") {
		t.Fatalf("the call is not read at all:\n%s", view)
	}
	if !strings.Contains(view, "rows more") {
		t.Errorf("the box does not say the call continues:\n%s", view)
	}
	if strings.Contains(view, "the last line of what the call wrote") {
		t.Errorf("the whole call fit a box that said it had more below it:\n%s", view)
	}

	m.Update(last) // to the bottom of the call
	bottom := stripCodes(m.View())
	if !strings.Contains(bottom, "the last line of what the call wrote") {
		t.Errorf("scrolling to the end did not bring up the last of the call:\n%s", bottom)
	}
	if strings.Contains(bottom, "rows more") {
		t.Errorf("the box still says there is more, at the end:\n%s", bottom)
	}
}

// the pane next to the list says what the call was asked for in words, so a call can be recognised without
// reading the JSON the model sent
func TestThePaneReadsTheCallRatherThanItsJSON(t *testing.T) {
	m := historyModel(t)
	view := openHistory(t, m, "/history")

	for _, want := range []string{"command: go test ./internal/tui/", "timeout: 240"} {
		if !strings.Contains(view, want) {
			t.Errorf("the pane lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, `{"`) {
		t.Errorf("the pane shows the arguments as the model wrote them:\n%s", view)
	}
}

// a call named by id opens already read in full, because that is what was asked for; nothing is printed
func TestACallAskedForByIDOpensReadInFull(t *testing.T) {
	m := historyModel(t)
	view := openHistory(t, m, "/history r1")

	if m.picker == nil {
		t.Fatal("a call asked for by id did not open the box")
	}
	for _, want := range []string{"r1 read_file", "path: main.go", "asked for:"} {
		if !strings.Contains(view, want) {
			t.Errorf("the call by id lacks %q:\n%s", want, view)
		}
	}
	if out := m.log.render(92); strings.Contains(out, "asked for:") {
		t.Errorf("the call by id was printed into the conversation:\n%s", out)
	}
}

// a run that has done nothing yet says so instead of opening an empty box
func TestHistoryWithNothingLoggedSaysSo(t *testing.T) {
	dir, conversation := t.TempDir(), "s20260101-120000.000"
	a := agent.New(nil, nil, "test-model", agent.Options{Audit: audit.New(dir, conversation), Root: dir}, nil)
	m := newModel(context.Background(), a, NewHub(), config.Config{Root: dir, Session: conversation})
	m.Update(tea.WindowSizeMsg{Width: 92, Height: 24})
	view := openHistory(t, m, "/history")

	if m.picker != nil {
		t.Error("an empty log opened an empty list")
	}
	if !strings.Contains(view, "nothing has been logged yet") {
		t.Errorf("an empty log was not said so:\n%s", view)
	}
}
