package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/audit"
	"github.com/yuriy-kovalchuk/ykode/internal/home"
)

// scratchHome points $YKODE_HOME at a scratch directory; startup_test.go uses it too.
func scratchHome(t *testing.T) (string, string) {
	t.Helper()
	homeDir, root := t.TempDir(), t.TempDir()
	t.Setenv("YKODE_HOME", homeDir)
	home.Reset()
	t.Cleanup(home.Reset)
	return homeDir, root
}

func logCalls(t *testing.T, homeDir, session, root string, entries ...audit.Entry) {
	t.Helper()
	logger := audit.New(filepath.Join(homeDir, "audit"), session)
	for _, e := range entries {
		e.Root = root
		if err := logger.Append(e); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHistoryWithoutFlagsShowsTheNewestRunOnly(t *testing.T) {
	homeDir, root := scratchHome(t)
	logCalls(t, homeDir, "s1", root, audit.Entry{ID: "r1", Tool: "read_file", Status: "ok", Time: time.Now().Add(-time.Hour)})
	logCalls(t, homeDir, "s2", root, audit.Entry{ID: "r3", Tool: "exec_command", Status: "error", Time: time.Now()})
	var out bytes.Buffer
	if err := historyCommand([]string{"--root", root}, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, "r3") || strings.Contains(got, "r1") {
		t.Errorf("wanted the newest run alone: %s", got)
	}
}

func TestHistoryAllJSONSinceAndFilters(t *testing.T) {
	homeDir, root := scratchHome(t)
	old := audit.Entry{ID: "r1", Tool: "exec_command", Status: "ok", Time: time.Now().Add(-48 * time.Hour)}
	recent := audit.Entry{ID: "r2", Tool: "edit_file", Status: "refused", Time: time.Now()}
	logCalls(t, homeDir, "s1", root, old, recent)
	logCalls(t, homeDir, "s2", root, recent)

	var all bytes.Buffer
	if err := historyCommand([]string{"--root", root, "--all"}, &all); err != nil {
		t.Fatal(err)
	}
	if got := all.String(); !strings.Contains(got, "r1") || !strings.Contains(got, "r2") {
		t.Errorf("--all should span both runs: %s", got)
	}
	var fresh bytes.Buffer
	if err := historyCommand([]string{"--root", root, "--all", "--since", "24h"}, &fresh); err != nil {
		t.Fatal(err)
	}
	if got := fresh.String(); strings.Contains(got, "r1") {
		t.Errorf("--since 24h should drop the two-day-old call: %s", got)
	}
	var failed bytes.Buffer
	if err := historyCommand([]string{"--root", root, "--all", "--failed"}, &failed); err != nil {
		t.Fatal(err)
	}
	if got := failed.String(); !strings.Contains(got, "r2") {
		t.Errorf("--failed: %s", got)
	}
	var asJSON bytes.Buffer
	if err := historyCommand([]string{"--root", root, "--all", "--json", "exec"}, &asJSON); err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(strings.NewReader(asJSON.String()))
	var e audit.Entry
	if err := dec.Decode(&e); err != nil {
		t.Fatalf("not JSON lines: %v (%s)", err, asJSON.String())
	}
	if e.Tool != "exec_command" {
		t.Errorf("the filter should have kept only exec: %+v", e)
	}
	if strings.Contains(asJSON.String(), "edit_file") {
		t.Errorf("the filter leaked: %s", asJSON.String())
	}
}

func TestHistoryRefusesADurationItCannotRead(t *testing.T) {
	_, root := scratchHome(t)
	if err := historyCommand([]string{"--root", root, "--since", "yesterday"}, nil); err == nil ||
		!strings.Contains(err.Error(), "--since") {
		t.Errorf("want an error about --since, got %v", err)
	}
}
