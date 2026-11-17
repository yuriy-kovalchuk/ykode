package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/home"
)

// scratchHome points $YKODE_HOME at a scratch directory and returns it with a project root.
func scratchHome(t *testing.T) (string, string) {
	t.Helper()
	homeDir, root := t.TempDir(), t.TempDir()
	t.Setenv("YKODE_HOME", homeDir)
	home.Reset()
	t.Cleanup(home.Reset)
	return homeDir, root
}

func TestStartupAgesOutWhatOldRunsLeftBehind(t *testing.T) {
	_, root := scratchHome(t)
	cfg, err := config.Load([]string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-400 * 24 * time.Hour)
	spill := filepath.Join(cfg.CommandLogDir, "ykode-exec-old-run")
	if err := os.MkdirAll(spill, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(spill, "output.txt"), []byte("output an old run cut off"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(spill, old, old); err != nil {
		t.Fatal(err)
	}

	PrepareHome(cfg)

	if _, err := os.Stat(spill); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a finished run's cut command log survived startup: %v", err)
	}

	if _, err := os.Stat(home.Settings()); err != nil {
		t.Errorf("a first run left no settings file to edit: %v", err)
	}
}

// a saved result ages out with its conversation, not with a command log's clock
func TestAnOldConversationTakesItsSavedResultsWithIt(t *testing.T) {
	_, root := scratchHome(t)
	cfg, err := config.Load([]string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-400 * 24 * time.Hour)

	if err := os.MkdirAll(cfg.SessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	session := filepath.Join(cfg.SessionDir, "s20000101-000000.jsonl")
	if err := os.WriteFile(session, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	young := filepath.Join(cfg.SessionDir, "s20260101-000000.jsonl")
	if err := os.WriteFile(young, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{session} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}

	PrepareHome(cfg)

	if _, err := os.Stat(session); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a conversation past its keep survived: %v", err)
	}
	if _, err := os.Stat(young); err != nil {
		t.Errorf("a conversation still inside its keep was removed: %v", err)
	}
}

func TestTheConversationBeingResumedIsNeverAgedOut(t *testing.T) {
	_, root := scratchHome(t)
	cfg, err := config.Load([]string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.SessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	resumed := filepath.Join(cfg.SessionDir, "s20000101-000000.jsonl")
	if err := os.WriteFile(resumed, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-400 * 24 * time.Hour)
	if err := os.Chtimes(resumed, old, old); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load([]string{"--root", root, "--continue", "s20000101-000000"})
	if err != nil {
		t.Fatal(err)
	}

	PrepareHome(cfg)

	if _, err := os.Stat(resumed); err != nil {
		t.Errorf("startup deleted the conversation --continue asked for: %v", err)
	}
}
