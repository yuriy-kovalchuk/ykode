package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/home"
)

// the first-run answer lands in a fresh home as the seeded file plus the server
func TestSavingTheEndpointToAFreshHome(t *testing.T) {
	fakeHome(t)
	path, err := SaveEndpoint("http://gpu.local:8080/v1", "sk-1", "qwen")
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("settings.yaml with a key in it: %v, mode %v", err, info.Mode().Perm())
	}
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "http://gpu.local:8080/v1" || cfg.APIKey != "sk-1" || cfg.Model != "qwen" || !cfg.EndpointSet {
		t.Errorf("the saved server did not come back: %q %q %q set=%v", cfg.BaseURL, cfg.APIKey, cfg.Model, cfg.EndpointSet)
	}
	if cfg.ReadLines != 150 || cfg.ThinkingLevel != "medium" {
		t.Error("the rest of the seeded defaults did not come with it")
	}
}

// a file the user wrote is edited in place: comments and other keys stay, the server is replaced
func TestSavingTheEndpointKeepsWhatTheUserWrote(t *testing.T) {
	fakeHome(t)
	writeSettings(t, home.Settings(), "# my laptop setup\nbase_url: http://old:1/v1 # the old box\napi_key: sk-keep\nmax_turns: 40\n")
	if _, err := SaveEndpoint("http://new:2/v1", "", "m2"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(home.Settings())
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"# my laptop setup", "max_turns: 40", "base_url: http://new:2/v1", "model: m2", "api_key: sk-keep"} {
		if !strings.Contains(text, want) {
			t.Errorf("after saving, the file lost %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "http://old:1/v1") {
		t.Errorf("the old server is still there:\n%s", text)
	}
}

// a home that only has the old settings.json keeps its settings when the answer is saved
func TestSavingTheEndpointCarriesALegacyJSONFileOver(t *testing.T) {
	fakeHome(t)
	legacy := filepath.Join(filepath.Dir(home.Settings()), "settings.json")
	writeSettings(t, legacy, `{"max_turns": 33, "thinking_level": "low"}`)
	if _, err := SaveEndpoint("http://gpu:3/v1", "", ""); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxTurns != 33 || cfg.ThinkingLevel != "low" || cfg.BaseURL != "http://gpu:3/v1" {
		t.Errorf("the legacy settings were lost on the way to YAML: turns=%d level=%q base=%q", cfg.MaxTurns, cfg.ThinkingLevel, cfg.BaseURL)
	}
	if raw, _ := os.ReadFile(home.Settings()); strings.Contains(string(raw), "{") {
		t.Errorf("the carried-over file still reads as JSON:\n%s", raw)
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
