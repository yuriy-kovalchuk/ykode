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

// TestMain keeps the package away from the developer's own ~/.ykode: a settings file that is
// really there would answer for every test below.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ykode-config-home")
	if err != nil {
		panic(err)
	}
	os.Setenv("YKODE_HOME", dir)
	home.Reset()
	code := m.Run()
	os.RemoveAll(dir)
	home.Reset()
	os.Exit(code)
}

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
		MaxTokens:       0, // no cap: the window decides
		SafetyMargin:    defaultSafety,
		ThinkingLevel:   defaultThinkingLevel,
		ReadLines:       defaultReadLines,
		SearchLimit:     defaultSearchLimit,
		ExecTimeout:     defaultExecTimeout,
		StreamIdle:      defaultStreamIdle,
		KeepCommandLogs: defaultLogKeep,
		KeepSessions:    defaultSessionKeep,
		KeepAudit:       defaultAuditKeep,
		LogAudit:        true,
	}
	cfg.CommandLogDir = "" // derived from $YKODE_HOME, checked separately
	cfg.ResultDir = ""     // derived from $YKODE_HOME and stamped with the run time
	cfg.AuditDir = ""      // derived from $YKODE_HOME
	cfg.SessionDir = ""    // derived from $YKODE_HOME
	cfg.SessionFile = ""   // derived from $YKODE_HOME and the run time
	cfg.Session = ""       // derived from the run time
	cfg.modelReasoning = nil
	cfg.modelSampling = nil
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
	if cfg.ThinkingLevel != "off" || !cfg.AssumeYes {
		t.Fatalf("thinking level = %q, AssumeYes=%v, want --no-think to mean off and yes on", cfg.ThinkingLevel, cfg.AssumeYes)
	}
}

// a cap of zero is the default and means the window decides where a reply ends
func TestZeroMaxTokensMeansNoCap(t *testing.T) {
	cfg, err := Load([]string{"--root", t.TempDir(), "--max-tokens", "0"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxTokens != 0 {
		t.Errorf("max_tokens = %d, want no cap", cfg.MaxTokens)
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
	if cfg.ThinkingLevel == "off" {
		t.Fatalf("thinking level = %q: thinking is on unless --no-think says otherwise", cfg.ThinkingLevel)
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
		{"negative cap", []string{"--max-tokens", "-1"}, "--max-tokens must be 0 (no cap)"},
		{"negative ctx", []string{"--ctx", "-5"}, "--ctx must be 0 (auto) or a positive token count"},
		{"invented thinking level", []string{"--thinking", "enormous"}, "want one of off, minimal, low, medium, high, xhigh, max"},
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
	if !sessionIDPattern.MatchString(name) {
		t.Fatalf("run dir %q is not stamped like s20060102-150405.000", name)
	}
}

func tempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("YKODE_HOME", dir)
	home.Reset()
	t.Cleanup(home.Reset)
	return dir
}

func TestContinueLastPicksTheNewestConversation(t *testing.T) {
	homeDir := tempHome(t)
	sessions := filepath.Join(homeDir, "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"s20260101-120000", "s20260103-090000"} {
		if err := os.WriteFile(filepath.Join(sessions, id+".jsonl"), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := Load([]string{"--continue", "last"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Session != "s20260103-090000" {
		t.Errorf("resuming %q, want the newest conversation", cfg.Session)
	}
	if cfg.SessionFile != filepath.Join(sessions, "s20260103-090000.jsonl") {
		t.Errorf("session file is %q", cfg.SessionFile)
	}
	if filepath.Base(cfg.ResultDir) != "s20260103-090000" {
		t.Errorf("a resumed run should reuse its results dir, got %q", cfg.ResultDir)
	}
}

func TestContinueWithNothingSavedSaysWhereItLooked(t *testing.T) {
	homeDir := tempHome(t)
	if _, err := Load([]string{"--continue", "last"}); err == nil || !strings.Contains(err.Error(), filepath.Join(homeDir, "sessions")) {
		t.Fatalf("want the empty directory named, got %v", err)
	}
}

func TestContinueRejectsAnythingThatIsNotAnID(t *testing.T) {
	tempHome(t)
	for _, want := range []string{"../../etc/passwd", "latest"} {
		if _, err := Load([]string{"--continue", want}); err == nil || !strings.Contains(err.Error(), "not a session id") {
			t.Errorf("--continue %q: want it rejected as an id, got %v", want, err)
		}
	}
	if _, err := Load([]string{"--continue", "s20060102-150405"}); err == nil || !strings.Contains(err.Error(), "no such conversation") {
		t.Errorf("an unknown but well-formed id: %v", err)
	}
}

func TestTwoRunsInTheSameSecondGetTheirOwnConversation(t *testing.T) {
	user := fakeHome(t)
	first, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !sessionIDPattern.MatchString(first.Session) || !strings.Contains(first.Session, ".") {
		t.Fatalf("session id %q carries no milliseconds", first.Session)
	}
	// an id from before milliseconds still resumes
	if err := os.MkdirAll(filepath.Join(user, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(user, "sessions", "s20260101-000000.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load([]string{"--continue", "s20260101-000000"}); err != nil {
		t.Errorf("an old id no longer resumes: %v", err)
	}
}

func TestEveryFlagReachesItsSetting(t *testing.T) {
	tempHome(t)
	cfg, err := Load([]string{"--base-url", "http://h/v1/", "--api-key", "k", "--model", "m", "--max-turns", "3",
		"--max-tokens", "99", "--ctx", "5000", "--no-think", "--yes", "--no-verify", "--send-reasoning",
		"-p", "task", "--json", "--version"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "http://h/v1" || cfg.APIKey != "k" || cfg.Model != "m" || cfg.MaxTurns != 3 ||
		cfg.MaxTokens != 99 || cfg.CtxSize != 5000 || cfg.ThinkingLevel != "off" || !cfg.AssumeYes || !cfg.NoVerify ||
		!cfg.SendReasoning || cfg.Prompt != "task" || !cfg.JSON || !cfg.Version {
		t.Errorf("flags did not all land: %+v", cfg)
	}
	off, err := Load([]string{"--yes=false", "--no-think=false"})
	if err != nil {
		t.Fatal(err)
	}
	if off.AssumeYes || off.ThinkingLevel == "off" {
		t.Errorf("a flag given as false switched its setting: yes=%v thinking=%v", off.AssumeYes, off.ThinkingLevel)
	}
}

// thinking is a depth, not a switch: the same names pi uses
func TestTheThinkingLevelSetsTheDepth(t *testing.T) {
	cfg, err := Load([]string{"--root", t.TempDir(), "--thinking", "low"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ThinkingLevel != "low" {
		t.Errorf("thinking level = %q, want low", cfg.ThinkingLevel)
	}
}

func TestNoThinkIsShorthandForTheOffLevel(t *testing.T) {
	cfg, err := Load([]string{"--root", t.TempDir(), "--no-think"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ThinkingLevel != "off" {
		t.Errorf("thinking level = %q, want off", cfg.ThinkingLevel)
	}
}
