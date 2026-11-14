package tui

import (
	"context"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// toolsModel is a session with the real tools wired in, so the list is what a working run shows.
func toolsModel(t *testing.T) *model {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := agent.New(nil, tools.BuiltIn(root, dir, tools.Options{}), "test-model", agent.Options{Root: dir}, nil)
	m := newModel(context.Background(), a, NewHub(), config.Config{Root: dir})
	m.Update(tea.WindowSizeMsg{Width: 92, Height: 24})
	return m
}

// /tools lists what the model may call and what each description costs it
func TestToolsListsWhatTheModelMayCall(t *testing.T) {
	m := toolsModel(t)
	m.input.SetValue("/tools")
	m.submit()

	if m.picker == nil {
		t.Fatal("/tools did not open the list")
	}
	view := stripCodes(m.View())
	for _, want := range []string{"ykode · tools", "read_file", "edit_file", "bytes"} {
		if !strings.Contains(view, want) {
			t.Errorf("the tools list lacks %q:\n%s", want, view)
		}
	}
}

// typing finds the tool you half-remember, which the printed column could not
func TestToolsNarrowsToTheOneYouAreLookingFor(t *testing.T) {
	m := toolsModel(t)
	m.input.SetValue("/tools")
	m.submit()
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("edit")})

	view := stripCodes(m.View())
	if !strings.Contains(view, "edit_file") || strings.Contains(view, "read_file") {
		t.Errorf("the narrowed list still holds the rest:\n%s", view)
	}
}

// there is nothing to choose in this list: the box stays up, and says so
func TestToolsHasNothingToChoose(t *testing.T) {
	m := toolsModel(t)
	m.input.SetValue("/tools")
	m.submit()
	m.Update(enter)

	if m.picker == nil {
		t.Fatal("enter closed a list that has nothing to choose")
	}
	if view := stripCodes(m.View()); !strings.Contains(view, "nothing to choose") {
		t.Errorf("the box does not say what it is for:\n%s", view)
	}
	m.Update(esc)
	if m.picker != nil {
		t.Error("esc did not give the session back")
	}
}
