package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/home"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/session"
)

func sessionsHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("YKODE_HOME", dir)
	home.Reset()
	t.Cleanup(home.Reset)
	return dir
}

func saveConversation(t *testing.T, dir, id, root, ask string) {
	t.Helper()
	log := session.New(filepath.Join(dir, "sessions"), id, root)
	for _, m := range []llm.Message{{Role: "user", Content: ask}, {Role: "assistant", Content: "done"}} {
		if err := log.Append(m); err != nil {
			t.Fatal(err)
		}
	}
}

// the listing names each conversation by what it was about, newest first, and says how to come back
func TestSessionsListsWhatCanBeResumed(t *testing.T) {
	dir := sessionsHome(t)
	saveConversation(t, dir, "s20260101-120000.000", "/home/dev/old-project", "split the parser")
	saveConversation(t, dir, "s20260102-090000.000", "/home/dev/ykode", "rename the helper\nand keep the tests green")

	var out bytes.Buffer
	if err := Sessions(nil, &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"s20260102-090000.000", "rename the helper and keep the tests green", "ykode", "--continue", "2 conversations"} {
		if !strings.Contains(got, want) {
			t.Errorf("the listing is missing %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "s20260102") > strings.Index(got, "s20260101") {
		t.Error("the newest conversation is not first")
	}
}

// nothing saved yet says where it looked, rather than an empty screen
func TestSessionsSaysWhereItLookedWhenNothingIsSaved(t *testing.T) {
	dir := sessionsHome(t)
	var out bytes.Buffer
	if err := Sessions(nil, &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "nothing saved") || !strings.Contains(got, filepath.Join(dir, "sessions")) {
		t.Errorf("empty listing:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "sessions")); err == nil {
		t.Error("an empty listing should not need the folder to exist")
	}
}
