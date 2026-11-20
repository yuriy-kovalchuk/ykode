package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// a setting returns to the built-in by taking the key out, the only on-disk meaning of "use the default"
func TestASettingGoesBackToItsDefaultByRemovingTheKey(t *testing.T) {
	user := fakeHome(t)
	path := filepath.Join(user, "settings.yaml")
	writeSettings(t, path, "# mine\nmodel: qwen3-coder\nmax_turns: 9\ncontext:\n    safety_margin: 9000\n")

	if _, _, err := Set(t.TempDir(), "context.safety_margin", "unset"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Contains(body, "safety_margin") || strings.Contains(body, "context:") {
		t.Errorf("the key or the section it lived in was left behind:\n%s", body)
	}
	for _, keep := range []string{"# mine", "model: qwen3-coder", "max_turns: 9"} {
		if !strings.Contains(body, keep) {
			t.Errorf("%s was lost by taking the other key back:\n%s", keep, body)
		}
	}

	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SafetyMargin != defaultSafety {
		t.Errorf("safety margin = %d, want the built-in %d", cfg.SafetyMargin, defaultSafety)
	}

	if _, _, err := Set(t.TempDir(), "assume_yes", "unset"); err == nil {
		t.Error("taking back a key the file never set passed silently")
	}
}

// an empty value in a file is not a choice: the table told the user they had chosen the resolved folder
func TestAKeyWrittenEmptyIsNotReportedAsChosen(t *testing.T) {
	fakeHome(t)
	writeSettings(t, filepath.Join(os.Getenv("YKODE_HOME"), "settings.yaml"), "tools:\n    command_log_dir: \"\"\n    read_lines: 400\n")
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CommandLogDir == "" {
		t.Fatal("an empty log dir was left empty rather than resolved")
	}
	global, project := fileKeys(cfg)
	if global["tools.command_log_dir"] || project["tools.command_log_dir"] {
		t.Error("a key left empty was counted as one the file set")
	}
	if !global["tools.read_lines"] {
		t.Error("a key with a value in it stopped being counted")
	}
}

// a change is written where it belongs, and the file's comments and other keys survive it
func TestASettingIsWrittenAndTheRestOfTheFileSurvives(t *testing.T) {
	user := fakeHome(t)
	path := filepath.Join(user, "settings.yaml")
	writeSettings(t, path, "# mine\nmodel: qwen3-coder\nmax_turns: 9\n")

	if _, _, err := Set(t.TempDir(), "assume_yes", "true"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, keep := range []string{"# mine", "model: qwen3-coder", "max_turns: 9"} {
		if !strings.Contains(body, keep) {
			t.Errorf("%s was lost by the change:\n%s", keep, body)
		}
	}

	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := findSetting(cfg.Catalogue(), "assume_yes")
	if got.Value != "on" || got.From != FromFile {
		t.Errorf("assume_yes = %s from %s, want on from file", got.Value, got.From)
	}
}

// a number and a switch are written as the file spells them, not as words in quotes
func TestANumberIsWrittenAsANumberAndASwitchAsASwitch(t *testing.T) {
	fakeHome(t)
	if _, _, err := Set(t.TempDir(), "max_turns", "40"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Set(t.TempDir(), "assume_yes", "on"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(os.Getenv("YKODE_HOME"), "settings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "max_turns: 40") || !strings.Contains(body, "assume_yes: true") {
		t.Errorf("written as:\n%s", body)
	}
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxTurns != 40 || !cfg.AssumeYes {
		t.Errorf("read back as max_turns=%d assume_yes=%v", cfg.MaxTurns, cfg.AssumeYes)
	}
}

// a key inside a section is written inside that section
func TestASectionKeyIsWrittenInsideItsSection(t *testing.T) {
	fakeHome(t)
	if _, _, err := Set(t.TempDir(), "tools.read_lines", "40"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReadLines != 40 {
		t.Errorf("read_lines = %d, want 40", cfg.ReadLines)
	}
}

// a value the knob cannot use is refused, and the file is left alone
func TestAValueThatWouldNotWorkIsRefused(t *testing.T) {
	user := fakeHome(t)
	path := filepath.Join(user, "settings.yaml")
	writeSettings(t, path, "stream_idle_sec: 120\n")

	for _, ask := range []struct{ key, value string }{
		{"stream_idle_sec", "soon"},
		{"thinking_level", "sideways"},
		{"theme", "neon"},
		{"assume_yes", "maybe"},
		{"max_turns", "-5"},
		{"keep_sessions", "a week"},
		{"no_such_thing", "1"},
	} {
		if _, _, err := Set(t.TempDir(), ask.key, ask.value); err == nil {
			t.Errorf("%s %s was accepted", ask.key, ask.value)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "stream_idle_sec: 120\n" {
		t.Errorf("the file was rewritten anyway:\n%s", raw)
	}
}

// the never-ask list grows one item at a time, and says so when the item is already there
func TestTheNeverAskListGrowsOneAtATime(t *testing.T) {
	fakeHome(t)
	if _, _, err := Set(t.TempDir(), "permissions.allow", "exec:go test"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Set(t.TempDir(), "permissions.allow", "exec:go test"); err == nil {
		t.Error("the same item was added twice")
	}
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Allow) != 1 || cfg.Allow[0] != "exec:go test" {
		t.Errorf("allow = %v, want the one item", cfg.Allow)
	}
}

// a change lands in the file that holds a token, and that file stays readable only by you
func TestTheFileWrittenStaysPrivate(t *testing.T) {
	user := fakeHome(t)
	if _, _, err := Set(t.TempDir(), "model", "qwen3-coder"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(user, "settings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("settings.yaml is mode %o, want 600", info.Mode().Perm())
	}
}

// an empty entry would stand for every request, so it never goes on the list
func TestAnEmptyEntryIsNotAddedToTheList(t *testing.T) {
	fakeHome(t)
	if _, _, err := Set(t.TempDir(), "permissions.allow", ""); err == nil {
		t.Error("an empty name was accepted for permissions.allow")
	}
}
