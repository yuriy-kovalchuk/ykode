package config

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/home"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		BaseURL:         defaultBaseURL,
		Root:            cwd,
		MaxTurns:        defaultMaxTurns,
		MaxTokens:       defaultMaxTokens,
		SafetyMargin:    defaultSafety,
		Think:           true,
		ReadLines:       defaultReadLines,
		SearchLimit:     defaultSearchLimit,
		ExecTimeout:     defaultExecTimeout,
		KeepCommandLogs: defaultLogKeep,
	}
	cfg.CommandLogDir = "" // derived from $YKODE_HOME, checked separately
	cfg.ResultDir = ""     // derived from $YKODE_HOME and stamped with the run time
	cfg.modelReasoning = nil
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("cfg = %+v, want %+v", cfg, want)
	}
}

func TestLoadFlagsOverrideEnv(t *testing.T) {
	t.Setenv("LOCAL_LLM_URL", "http://from-env:9/v1")
	t.Setenv("LOCAL_LLM_MODEL", "env-model")
	dir := t.TempDir()

	cfg, err := Load([]string{
		"--base-url", "https://from-flag/v1/",
		"--model", "flag-model",
		"--root", dir,
		"--max-turns", "3",
		"--max-tokens", "1024",
		"--ctx", "8192",
		"--no-think",
		"--yes",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://from-flag/v1" {
		t.Fatalf("base URL = %q, want the flag to win and the slash to go", cfg.BaseURL)
	}
	if cfg.Model != "flag-model" {
		t.Fatalf("model = %q", cfg.Model)
	}
	if cfg.Root != dir || !filepath.IsAbs(cfg.Root) {
		t.Fatalf("root = %q, want %q", cfg.Root, dir)
	}
	if cfg.MaxTurns != 3 || cfg.MaxTokens != 1024 || cfg.CtxSize != 8192 {
		t.Fatalf("counts = %+v", cfg)
	}
	if cfg.Think || !cfg.AssumeYes {
		t.Fatalf("Think=%v AssumeYes=%v, want thinking off and yes on", cfg.Think, cfg.AssumeYes)
	}
}

func TestLoadEnvFallbacks(t *testing.T) {
	t.Setenv("LOCAL_LLM_URL", "http://env-url/v1")
	t.Setenv("LOCAL_LLM_MODEL", "env-model")

	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "http://env-url/v1" || cfg.Model != "env-model" {
		t.Fatalf("cfg = %+v, want both env vars honoured", cfg)
	}
	if !cfg.Think {
		t.Fatal("thinking is on unless --no-think says otherwise")
	}
}

func TestLoadMakesARelativeRootAbsolute(t *testing.T) {
	dir := t.TempDir()
	here, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(here)

	cfg, err := Load([]string{"--root", "."})
	if err != nil {
		t.Fatal(err)
	}
	// Getwd is the resolved absolute form, which is what --root . must produce.
	where, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Root != where {
		t.Fatalf("root = %q, want %q", cfg.Root, where)
	}
}

func TestLoadRejectsBadSettings(t *testing.T) {
	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no turns", []string{"--max-turns", "0"}, "--max-turns must be at least 1"},
		{"no tokens", []string{"--max-tokens", "-1"}, "--max-tokens must be at least 1"},
		{"negative ctx", []string{"--ctx", "-5"}, "--ctx must be 0 (auto) or a positive token count"},
		{"missing root", []string{"--root", filepath.Join(t.TempDir(), "nope")}, "nope"},
		{"root is a file", []string{"--root", file}, "not a directory"},
		{"unknown flag", []string{"--nope"}, "flag provided but not defined"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Load(tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestLoadReportsHelp(t *testing.T) {
	if _, err := Load([]string{"-h"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("error = %v, want flag.ErrHelp", err)
	}
}

func TestTheApiKeyComesFromTheEnvOrTheFlag(t *testing.T) {
	t.Setenv("LOCAL_LLM_API_KEY", "env-key")
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "env-key" {
		t.Fatalf("APIKey = %q, want the env fallback", cfg.APIKey)
	}
	cfg, err = Load([]string{"--api-key", "flag-key"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "flag-key" {
		t.Fatalf("APIKey = %q, want the flag to win", cfg.APIKey)
	}
}

func TestTheVerifyAndReasoningSwitches(t *testing.T) {
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.NoVerify {
		t.Fatal("the verify nudge is on by default")
	}
	if cfg.SendReasoning {
		t.Fatal("thinking is not sent back unless asked")
	}
	cfg, err = Load([]string{"--no-verify", "--send-reasoning"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.NoVerify || !cfg.SendReasoning {
		t.Fatalf("NoVerify=%v SendReasoning=%v, want both switches to flip", cfg.NoVerify, cfg.SendReasoning)
	}
}

func TestTheOneShotFlags(t *testing.T) {
	cfg, err := Load([]string{"-p", "fix the flaky test", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Prompt != "fix the flaky test" || !cfg.JSON {
		t.Fatalf("Prompt=%q JSON=%v", cfg.Prompt, cfg.JSON)
	}
	cfg, err = Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Prompt != "" || cfg.JSON {
		t.Fatal("without -p ykode opens a session")
	}
}

func TestTheDefaultReadWindowKeepsSearchingFirst(t *testing.T) {
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ReadLines > 200 {
		t.Errorf("ReadLines = %d: a window this big lets the model dump files instead of searching", cfg.ReadLines)
	}
}

func TestResultsLiveUnderTheHomeDirForThisRun(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("YKODE_HOME", homeDir)
	home.Reset()
	t.Cleanup(home.Reset)
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if dir := filepath.Dir(cfg.ResultDir); dir != filepath.Join(homeDir, "results") {
		t.Fatalf("results sit in %s, want %s", dir, filepath.Join(homeDir, "results"))
	}
	name := filepath.Base(cfg.ResultDir)
	if !strings.HasPrefix(name, "s2") || len(name) != 16 {
		t.Fatalf("run dir %q is not stamped like s20060102-150405", name)
	}
}
