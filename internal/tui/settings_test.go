package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/home"
)

// settingsHome puts the settings file in a scratch folder for one test.
func settingsHome(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("YKODE_HOME", dir)
	home.Reset()
	t.Cleanup(home.Reset)
	if body != "" {
		if err := os.WriteFile(filepath.Join(dir, "settings.yaml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// rowFor finds the list row that starts with a key.
func rowFor(table, key string) string {
	for _, line := range strings.Split(table, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), key) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func settingsModel(t *testing.T) *model {
	t.Helper()
	m := newTestModel()
	m.cfg = config.Config{
		BaseURL: "http://127.0.0.1:8080/v1", ThinkingLevel: "medium", MaxTurns: 25,
		ReadLines: 150, SearchLimit: 50, SafetyMargin: 4096,
		ExecTimeout: time.Minute, StreamIdle: 2 * time.Minute,
		KeepSessions: 720 * time.Hour, KeepCommandLogs: 24 * time.Hour, KeepAudit: 4320 * time.Hour,
		// as a run that read the file below resolved it: the mouse was asked for there
		LogAudit: true, Mouse: true, Allow: []string{"exec:go test"},
	}
	return m
}

// the list shows every knob, and what someone changed stands out from what nobody chose
func TestSettingsListsEveryKnobAndWhatWasChanged(t *testing.T) {
	settingsHome(t, "# mine\nmouse: true\npermissions:\n  allow:\n    - exec:go test\n")
	m := settingsModel(t)

	out := stripCodes(m.settings("/settings"))
	for _, want := range []string{"base_url", "tools.read_lines", "audit_retention_days", "what this run reads"} {
		if !strings.Contains(out, want) {
			t.Errorf("the list is missing %q:\n%s", want, out)
		}
	}
	if line := rowFor(out, "mouse"); !strings.Contains(line, "on") || !strings.Contains(line, "was off · file") {
		t.Errorf("a knob someone changed is not marked as changed: %q", line)
	}
	if line := rowFor(out, "permissions.allow"); !strings.Contains(line, "exec:go test") {
		t.Errorf("the never-ask list is not shown: %q", line)
	}
	if line := rowFor(out, "model"); !strings.Contains(line, "unset") || strings.Contains(line, "was") {
		t.Errorf("a knob nobody set should read as unset and say nothing else: %q", line)
	}
}

// one knob explained: what it is for, what it is now, what it would be, its choices, how to change it
func TestOneKnobIsExplainedRatherThanListed(t *testing.T) {
	m := settingsModel(t)
	out := stripCodes(m.settings("/settings thinking_level"))
	for _, want := range []string{
		"how deeply the model thinks", "now medium · default medium · from default",
		"choices: off, minimal, low, medium, high, xhigh, max", "change it: /settings thinking_level VALUE",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}

// a change from the screen reaches the file, and the screen says which way it moved
func TestAChangeFromTheScreenLandsInTheFile(t *testing.T) {
	dir := settingsHome(t, "# keep me\nmodel: qwen3-coder\n")
	m := settingsModel(t)

	out := stripCodes(m.settings("/settings tools.read_lines 40"))
	if !strings.Contains(out, "read_lines: 150 → 40") {
		t.Errorf("the change was not reported as a move:\n%s", out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "settings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "read_lines: 40") || !strings.Contains(body, "# keep me") || !strings.Contains(body, "model: qwen3-coder") {
		t.Errorf("the file after the change:\n%s", body)
	}
}

// a value the knob cannot take is refused and the file is left as it was
func TestABadValueIsRefusedWithoutTouchingTheFile(t *testing.T) {
	dir := settingsHome(t, "stream_idle_sec: 120\n")
	m := settingsModel(t)

	out := stripCodes(m.settings("/settings stream_idle_sec soon"))
	if !strings.Contains(out, "not changed") {
		t.Errorf("a bad value was accepted:\n%s", out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "settings.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "stream_idle_sec: 120\n" {
		t.Errorf("the file was rewritten:\n%s", raw)
	}
}

// a knob nobody named gets the list of the ones there are
func TestAKnobNobodyNamedNamesTheOnesThereAre(t *testing.T) {
	settingsHome(t, "")
	m := settingsModel(t)
	out := stripCodes(m.settings("/settings mose"))
	if !strings.Contains(out, `no setting called mose`) || !strings.Contains(out, "mouse") {
		t.Errorf("unknown knob answered:\n%s", out)
	}
}

// the screen answers /settings itself, so it works while a turn is running
func TestSettingsIsAnsweredWhileATurnRuns(t *testing.T) {
	settingsHome(t, "")
	m := settingsModel(t)
	m.running = true
	m.input.SetValue("/settings mouse")
	m.submit()

	out := stripCodes(m.View())
	if !strings.Contains(out, "the screen takes the mouse") {
		t.Errorf("a running turn was not allowed to answer /settings:\n%s", out)
	}
}
