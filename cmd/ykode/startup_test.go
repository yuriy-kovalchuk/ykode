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
