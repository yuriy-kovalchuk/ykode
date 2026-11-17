package tui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// the terminal's tab says which project this ykode was opened for
func TestTheTitleNamesTheProjectFolder(t *testing.T) {
	for _, tc := range []struct{ root, want string }{
		{"/Users/yuriy/Developer/ykode", "ykode: ykode"},
		{"/Users/yuriy/Developer/ykode/", "ykode: ykode"}, // a trailing slash is not a folder of its own
		{"/", "ykode: /"},
		{"", "ykode"}, // no project decided yet: the program alone
	} {
		if got := windowTitle(tc.root); got != tc.want {
			t.Errorf("windowTitle(%q) = %q, want %q", tc.root, got, tc.want)
		}
	}
}

// a folder name built to escape the title loses the characters that would close it early
func TestATitleBuiltToEscapeIsHarmless(t *testing.T) {
	hostile := "/srv/checkout\x1b]0;pwned\x07"
	if got, want := titleSequence(hostile), "\x1b]0;ykode: checkout]0;pwned\x07"; got != want {
		t.Errorf("titleSequence = %q, want %q", got, want)
	}
}

// the session names the terminal's tab before any key: an escape sequence, not screen text
func TestTheTabOfTheTerminalSaysWhichProjectItRuns(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tiny-api")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	hub := NewHub()
	a := agent.New(llm.NewClient("http://127.0.0.1:1/v1", ""), tools.NewRegistry(), "test-model",
		agent.Options{Root: root, MaxTurns: 2, MaxTokens: 64, CtxSize: 8000}, hub)
	// the program reads keys from a file descriptor, so an os pipe paces it the way a terminal does
	in, keys, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer keys.Close()
	defer in.Close()
	screen := &lockedBuffer{}

	done := make(chan error, 1)
	go func() {
		done <- run(context.Background(), a, hub, config.Config{}, screen, tea.WithInput(in), tea.WithOutput(screen))
	}()

	waitFor(t, screen, "\x1b]0;ykode: tiny-api\x07")
	writeKeys(t, keys, "/exit\r")
	if err := <-done; err != nil {
		t.Errorf("the session ended with an error: %v", err)
	}
}
