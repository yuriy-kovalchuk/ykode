package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/home"
)

// with no file of its own a change lands in the user's settings, and the project's folder is not
// created on the way
func TestAChangeWithoutAProjectFileGoesToTheGlobalOne(t *testing.T) {
	user := fakeHome(t)
	root := t.TempDir()

	path, local, err := Set(root, "mouse", "on")
	if err != nil {
		t.Fatal(err)
	}
	if local {
		t.Errorf("%s reported as the project's own file", path)
	}
	if want := filepath.Join(user, "settings.yaml"); path != want {
		t.Errorf("wrote %s, want the global file %s", path, want)
	}
	if _, err := os.Stat(filepath.Join(root, ".ykode")); !errors.Is(err, os.ErrNotExist) {
		t.Error("a change created a settings folder in the project")
	}
}

// what ykode wrote itself is not something the next start asks about
func TestCreateLocalWritesTheFileAndIsTrusted(t *testing.T) {
	fakeHome(t)
	root := t.TempDir()

	path, err := CreateLocal(root, "http://gpu.local:9/v1", "qwen")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"base_url: http://gpu.local:9/v1", "model: qwen"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("%s missing from the project file:\n%s", want, raw)
		}
	}
	if _, err := CreateLocal(root, "http://elsewhere:1/v1", ""); err == nil {
		t.Error("a second /create-local-setting wrote over the project's file")
	}

	cfg, err := Load([]string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.HeldKeys) != 0 {
		t.Errorf("a file ykode wrote is asked about again: %v", cfg.HeldKeys)
	}
	if cfg.BaseURL != "http://gpu.local:9/v1" {
		t.Errorf("the project's server did not take: %q", cfg.BaseURL)
	}
}

// a project with a file of its own takes every change, and the global file keeps the bytes it had
func TestAProjectFileTakesTheChanges(t *testing.T) {
	user := fakeHome(t)
	global := filepath.Join(user, "settings.yaml")
	writeSettings(t, global, "max_turns: 9\n")
	root := t.TempDir()
	if _, err := CreateLocal(root, "http://old:1/v1", ""); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(global)
	if err != nil {
		t.Fatal(err)
	}

	path, local, err := Set(root, "base_url", "http://gpu.local:9/v1")
	if err != nil {
		t.Fatal(err)
	}
	if !local || path != LocalSettings(root) {
		t.Errorf("the change went to %s (project=%v), want %s", path, local, LocalSettings(root))
	}
	after, err := os.ReadFile(global)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("the global file was rewritten:\n%s", after)
	}

	cfg, err := Load([]string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "http://gpu.local:9/v1" || len(cfg.HeldKeys) != 0 {
		t.Errorf("the project file did not answer for itself: base=%q held=%v", cfg.BaseURL, cfg.HeldKeys)
	}
	if cfg.MaxTurns != 9 {
		t.Errorf("a key only the global file sets stopped applying: max_turns %d", cfg.MaxTurns)
	}
}

// a project file someone edited by hand is a stranger again, however ykode came to write it
func TestAProjectFileEditedByHandIsAskedAboutAgain(t *testing.T) {
	fakeHome(t)
	root := t.TempDir()
	if _, err := CreateLocal(root, "http://old:1/v1", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Set(root, "max_turns", "40"); err != nil {
		t.Fatal(err)
	}
	writeSettings(t, LocalSettings(root), "base_url: http://attacker.example:9/v1\nmax_turns: 40\n")

	cfg, err := Load([]string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.HeldKeys) == 0 {
		t.Error("an edited project file was trusted without asking")
	}
	if cfg.BaseURL == "http://attacker.example:9/v1" {
		t.Error("the edited file set the server on its own")
	}
}

// a row of the catalogue says which file decided it: the project's, the user's own, or neither
func TestACatalogueRowSaysWhichFileSetIt(t *testing.T) {
	fakeHome(t)
	writeSettings(t, filepath.Join(home.Dir(), "settings.yaml"), "max_turns: 9\n")
	root := t.TempDir()
	if _, err := CreateLocal(root, "http://gpu.local:9/v1", ""); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load([]string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	base, ok := findSetting(cfg.Catalogue(), "base_url")
	if !ok || base.From != FromProject {
		t.Errorf("base_url came from %q, want the project file", base.From)
	}
	turns, ok := findSetting(cfg.Catalogue(), "max_turns")
	if !ok || turns.From != FromFile || turns.Value != "9" {
		t.Errorf("max_turns = %s from %q, want 9 from the user's own file", turns.Value, turns.From)
	}
}
