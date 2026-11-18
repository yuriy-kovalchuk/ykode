package config

import (
	"os"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/home"
)

// the first-run answer lands in a fresh home as the seeded file plus the server
func TestSavingTheEndpointToAFreshHome(t *testing.T) {
	fakeHome(t)
	path, _, err := SaveEndpoint(t.TempDir(), "http://gpu.local:8080/v1", "qwen")
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("a settings file written for one user: %v, mode %v", err, info.Mode().Perm())
	}
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "http://gpu.local:8080/v1" || cfg.Model != "qwen" || !cfg.EndpointSet {
		t.Errorf("the saved server did not come back: %q %q set=%v", cfg.BaseURL, cfg.Model, cfg.EndpointSet)
	}
	if cfg.ReadLines != 2000 || cfg.ThinkingLevel != "medium" {
		t.Error("the rest of the seeded defaults did not come with it")
	}
}

// a file the user wrote is edited in place: comments and other keys stay, the server is replaced
func TestSavingTheEndpointKeepsWhatTheUserWrote(t *testing.T) {
	fakeHome(t)
	writeSettings(t, home.Settings(), "# my laptop setup\nbase_url: http://old:1/v1 # the old box\nkeep_sessions: 24h\nmax_turns: 40\n")
	if _, _, err := SaveEndpoint(t.TempDir(), "http://new:2/v1", "m2"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(home.Settings())
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"# my laptop setup", "max_turns: 40", "base_url: http://new:2/v1", "model: m2", "keep_sessions: 24h"} {
		if !strings.Contains(text, want) {
			t.Errorf("after saving, the file lost %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "http://old:1/v1") {
		t.Errorf("the old server is still there:\n%s", text)
	}
}

// a server named anywhere - file, environment, flag - is a server someone chose
func TestAnyNamedServerCountsAsChosen(t *testing.T) {
	fakeHome(t)
	if cfg, _ := Load(nil); cfg.EndpointSet {
		t.Error("nothing named a server, yet it counts as chosen")
	}
	if cfg, _ := Load([]string{"--base-url", "http://flag:1/v1"}); !cfg.EndpointSet {
		t.Error("--base-url does not count")
	}
	t.Setenv("LOCAL_LLM_URL", "http://env:1/v1")
	if cfg, _ := Load(nil); !cfg.EndpointSet {
		t.Error("$LOCAL_LLM_URL does not count")
	}
}
