package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/home"
)

func TestStartupAgesOutWhatOldRunsLeftBehind(t *testing.T) {
	homeDir, root := scratchHome(t)
	cfg, err := config.Load([]string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-400 * 24 * time.Hour)
	stale := filepath.Join(cfg.AuditDir, "2000-01.jsonl")
	if err := os.MkdirAll(cfg.AuditDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	results := filepath.Join(homeDir, "results", "s20000101-000000")
	if err := os.MkdirAll(results, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(results, old, old); err != nil {
		t.Fatal(err)
	}

	prepareHome(cfg)

	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("an audit file past retention survived startup: %v", err)
	}
	if _, err := os.Stat(results); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a finished run's result dir survived startup: %v", err)
	}

	if _, err := os.Stat(home.Settings()); err != nil {
		t.Errorf("a first run left no settings file to edit: %v", err)
	}
}

// A saved result is only reachable by the conversation that names its id, so the two age out
// together: pruning results on a command log's clock leaves old sessions pointing at nothing.
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
	results := filepath.Join(filepath.Dir(cfg.ResultDir), "s20000101-000000")
	if err := os.MkdirAll(results, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(results, "r1.txt"), []byte("output"), 0o600); err != nil {
		t.Fatal(err)
	}
	young := filepath.Join(cfg.SessionDir, "s20260101-000000.jsonl")
	if err := os.WriteFile(young, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{session, results} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}

	prepareHome(cfg)

	if _, err := os.Stat(session); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a conversation past its keep survived: %v", err)
	}
	if _, err := os.Stat(results); !errors.Is(err, os.ErrNotExist) {
		t.Error("its saved results outlived the conversation that could name them")
	}
	if _, err := os.Stat(young); err != nil {
		t.Errorf("a conversation still inside its keep was removed: %v", err)
	}
}
