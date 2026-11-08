package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func saveSessions(t *testing.T, ids ...string) {
	t.Helper()
	user := fakeHome(t)
	for _, id := range ids {
		writeSettings(t, filepath.Join(user, "sessions", id+".jsonl"), "{}\n")
	}
}

// an id is a timestamp down to the millisecond, and nobody remembers one: enough of it to name one
// conversation is enough
func TestContinueAcceptsEnoughOfAnIDToBeUnique(t *testing.T) {
	saveSessions(t, "s20260101-120000.000", "s20260102-213711.728")

	cfg, err := Load([]string{"--continue", "2137"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Session != "s20260102-213711.728" {
		t.Errorf("--continue 2137 picked %q", cfg.Session)
	}
	if !strings.HasSuffix(cfg.SessionFile, "s20260102-213711.728.jsonl") {
		t.Errorf("the run reads %s", cfg.SessionFile)
	}
}

// an id lessay that could be two conversations says so and shows the ones it could be
func TestContinueSaysWhenAnIDEssayCouldBeTwo(t *testing.T) {
	saveSessions(t, "s20260101-120000.000", "s20260102-120000.000")

	_, err := Load([]string{"--continue", "120000"})
	if err == nil {
		t.Fatal("an ambiguous id was accepted")
	}
	for _, want := range []string{"s20260101-120000.000", "s20260102-120000.000"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %s: %v", want, err)
		}
	}
}

// something that could not be an id is refused, and a whole id that is not there is refused by name
func TestContinueRefusesWhatItCannotName(t *testing.T) {
	saveSessions(t, "s20260101-120000.000")
	for _, want := range []string{"x", "../sessions", "s20260303-030303.000"} {
		if _, err := Load([]string{"--continue", want}); err == nil {
			t.Errorf("--continue %q was accepted", want)
		}
	}
}
