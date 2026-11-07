package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/home"
)

// fakeHome points $YKODE_HOME at a scratch directory for one test.
func fakeHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("YKODE_HOME", dir)
	home.Reset()
	t.Cleanup(home.Reset)
	return dir
}

func writeSettings(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsSupplyWhatNoFlagSays(t *testing.T) {
	user := fakeHome(t)
	writeSettings(t, filepath.Join(user, "settings.json"), `{
		"base_url": "http://from-settings:8080/v1",
		"model": "settings-model",
		"max_turns": 7,
		"thinking": false,
		"verify": false,
		"context": {"safety_margin": 512, "reserve_output": 2048},
		"tools": {"read_lines": 120, "search_limit": 20, "exec_timeout_sec": 300, "keep_command_logs": "2h"},
		"models": {"qwen": {"send_reasoning": true}}
	}`)

	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "http://from-settings:8080/v1" || cfg.Model != "settings-model" {
		t.Fatalf("base/model = %q / %q", cfg.BaseURL, cfg.Model)
	}
	if cfg.MaxTurns != 7 || cfg.MaxTokens != 2048 || cfg.SafetyMargin != 512 {
		t.Fatalf("turns/max tokens/margin = %d/%d/%d", cfg.MaxTurns, cfg.MaxTokens, cfg.SafetyMargin)
	}
	if cfg.Think || !cfg.NoVerify {
		t.Fatalf("Think=%v NoVerify=%v, want the settings switches honoured", cfg.Think, cfg.NoVerify)
	}
	if cfg.ReadLines != 120 || cfg.SearchLimit != 20 || cfg.ExecTimeout != 300*time.Second || cfg.KeepCommandLogs != 2*time.Hour {
		t.Fatalf("tool settings = %+v", cfg)
	}
	if !cfg.SendReasoningFor("qwen") || cfg.SendReasoningFor("other") {
		t.Fatal("send_reasoning must be per model")
	}
	if cfg.CommandLogDir != filepath.Join(user, "logs") {
		t.Fatalf("command log dir = %q, want it under YKODE_HOME", cfg.CommandLogDir)
	}
}

func TestProjectSettingsOverrideTheUserOne(t *testing.T) {
	fakeHome(t)
	root := t.TempDir()
	writeSettings(t, filepath.Join(root, ".ykode", "settings.json"), `{"model": "project-model", "max_turns": 3}`)

	cfg, err := Load([]string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "project-model" || cfg.MaxTurns != 3 {
		t.Fatalf("model/turns = %q/%d, want the project file to win", cfg.Model, cfg.MaxTurns)
	}
}

func TestFlagsBeatSettingsAndEnv(t *testing.T) {
	user := fakeHome(t)
	writeSettings(t, filepath.Join(user, "settings.json"), `{"model": "settings-model", "max_turns": 7}`)
	t.Setenv("LOCAL_LLM_MODEL", "env-model")

	cfg, err := Load([]string{"--model", "flag-model"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "flag-model" {
		t.Fatalf("model = %q, want the flag to win over settings and env", cfg.Model)
	}
	if cfg.MaxTurns != 7 {
		t.Fatalf("max turns = %d, want the setting to survive an unrelated flag", cfg.MaxTurns)
	}
}

func TestEnvBeatsSettings(t *testing.T) {
	user := fakeHome(t)
	writeSettings(t, filepath.Join(user, "settings.json"), `{"base_url": "http://from-settings:8080/v1"}`)
	t.Setenv("LOCAL_LLM_URL", "http://from-env:8080/v1")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "http://from-env:8080/v1" {
		t.Fatalf("base URL = %q, want env to outrank settings.json", cfg.BaseURL)
	}
}

func TestABrokenSettingsFileNamesItself(t *testing.T) {
	user := fakeHome(t)
	path := filepath.Join(user, "settings.json")

	writeSettings(t, path, `{"max_turns": `)
	if _, err := Load(nil); err == nil || !strings.Contains(err.Error(), "settings.json") {
		t.Fatalf("Load() error = %v, want the bad JSON named with its file", err)
	}

	writeSettings(t, path, `{"tools": {"keep_command_logs": "a day"}}`)
	_, err := Load(nil)
	if err == nil || !strings.Contains(err.Error(), "keep_command_logs") {
		t.Fatalf("Load() error = %v, want the bad duration named", err)
	}
}

func TestAPermissionAllowListIsReadFromSettings(t *testing.T) {
	homeDir := fakeHome(t)
	write := func(path, text string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(homeDir, "settings.json"), `{"permissions":{"allow":["edit_file","exec:go test"]}}`)
	write(filepath.Join(homeDir, "project", ".ykode", "settings.json"), `{"permissions":{"allow":["exec:make"]}}`)

	cfg, err := Load([]string{"--root", filepath.Join(homeDir, "project")})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(cfg.Allow, ",")
	if !strings.Contains(got, "edit_file") || !strings.Contains(got, "exec:go test") || !strings.Contains(got, "exec:make") {
		t.Errorf("allow list = %q, want both files' entries", got)
	}
}

func TestTheAuditLogCanBeTurnedOffAndItsKeepShortened(t *testing.T) {
	user := fakeHome(t)
	writeSettings(t, filepath.Join(user, "settings.json"), `{"audit": false, "audit_retention_days": 30}`)
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogAudit {
		t.Error("audit: false should stop the log being written")
	}
	if cfg.KeepAudit != 30*24*time.Hour {
		t.Errorf("audit_retention_days: want 30 days, got %s", cfg.KeepAudit)
	}
}

func TestTheAuditLogIsOnAndKeptForSixMonthsByDefault(t *testing.T) {
	fakeHome(t)
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.LogAudit {
		t.Error("the audit log should be on unless a setting says otherwise")
	}
	if cfg.KeepAudit != 180*24*time.Hour {
		t.Errorf("default keep: want 180 days, got %s", cfg.KeepAudit)
	}
}

func TestANonsenseRetentionIsRefusedByName(t *testing.T) {
	user := fakeHome(t)
	writeSettings(t, filepath.Join(user, "settings.json"), `{"audit_retention_days": 0}`)
	if _, err := Load(nil); err == nil || !strings.Contains(err.Error(), "audit_retention_days") {
		t.Errorf("want an error naming audit_retention_days, got %v", err)
	}
}

func TestAKeyComesFromTheFileOrFromTheVariableItNames(t *testing.T) {
	user := fakeHome(t)
	t.Setenv("ROUTER_TOKEN", "from-the-variable")
	writeSettings(t, filepath.Join(user, "settings.json"),
		`{"api_key": "written-into-the-file", "api_key_env": "ROUTER_TOKEN"}`)
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "from-the-variable" {
		t.Errorf("the named variable should win: %q", cfg.APIKey)
	}

	writeSettings(t, filepath.Join(user, "settings.json"), `{"api_key": "written-into-the-file"}`)
	cfg, err = Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "written-into-the-file" {
		t.Errorf("api_key: %q", cfg.APIKey)
	}
}

func TestAVariableThatIsNotSetLeavesNoKey(t *testing.T) {
	user := fakeHome(t)
	writeSettings(t, filepath.Join(user, "settings.json"), `{"api_key_env": "NOT_SET_ANYWHERE"}`)
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "" {
		t.Errorf("a missing variable invented a key: %q", cfg.APIKey)
	}
}

func TestAKeyThatIsNotSpelledRightIsNamedInTheError(t *testing.T) {
	user := fakeHome(t)
	writeSettings(t, filepath.Join(user, "settings.json"), `{"base_ url": "http://nowhere"}`)
	_, err := Load(nil)
	if err == nil || !strings.Contains(err.Error(), "base_ url") {
		t.Errorf("want the misspelled key named, got %v", err)
	}
}

func TestAMisspelledKeyInAPortofTheFileIsNamedToo(t *testing.T) {
	user := fakeHome(t)
	writeSettings(t, filepath.Join(user, "settings.json"), `{"tools": {"read_lines ": 40}}`)
	_, err := Load(nil)
	if err == nil || !strings.Contains(err.Error(), "read_lines ") {
		t.Errorf("want the nested misspelling named, got %v", err)
	}
}
