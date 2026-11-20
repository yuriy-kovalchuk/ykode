package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/theme"
)

// every setting says what it is now, what it would be with nothing set, and who set it
func TestEachSettingSaysWhatItIsNowAndWhence(t *testing.T) {
	user := fakeHome(t)
	writeSettings(t, filepath.Join(user, "settings.yaml"), "assume_yes: true\npermissions:\n  allow:\n    - exec:go test\n")
	t.Setenv("LOCAL_LLM_URL", "http://from-env:8080/v1")

	cfg, err := Load([]string{"--max-turns", "9"})
	if err != nil {
		t.Fatal(err)
	}
	list := cfg.Catalogue()
	if len(list) < 14 {
		t.Fatalf("the list holds %d settings; the file has more to show", len(list))
	}

	want := []Setting{
		{Key: "assume_yes", Value: "on", Default: "off", From: FromFile},
		{Key: "max_turns", Value: "9", Default: "100", From: FromFlag},
		{Key: "base_url", Value: "http://from-env:8080/v1", Default: defaultBaseURL, From: FromEnv},
		{Key: "tools.read_lines", Value: "2000", Default: "2000", From: FromDefault},
		{Key: "permissions.allow", Value: "exec:go test", Default: "", From: FromFile},
	}
	for _, w := range want {
		got, ok := findSetting(list, w.Key)
		if !ok {
			t.Fatalf("%s is not on the list", w.Key)
		}
		if got.Value != w.Value || got.Default != w.Default || got.From != w.From {
			t.Errorf("%s = %s (default %s, from %s), want %s (default %s, from %s)",
				w.Key, got.Value, got.Default, got.From, w.Value, w.Default, w.From)
		}
		if got.Note == "" {
			t.Errorf("%s has no note to explain it", w.Key)
		}
	}
}

// a flag beats the environment, which beats a file: the list says the last one that spoke
func TestAFlagOutranksTheEnvironmentAndTheFile(t *testing.T) {
	user := fakeHome(t)
	writeSettings(t, filepath.Join(user, "settings.yaml"), "model: from-file\n")
	t.Setenv("LOCAL_LLM_MODEL", "from-env")

	cfg, err := Load([]string{"--model", "from-flag"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := findSetting(cfg.Catalogue(), "model")
	if got.From != FromFlag || got.Value != "from-flag" {
		t.Errorf("model = %s from %s, want from-flag from flag", got.Value, got.From)
	}
}

// the choices a knob accepts are on the list, so a screen can show them
func TestAKnobWithChoicesSaysWhatTheyAre(t *testing.T) {
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := findSetting(cfg.Catalogue(), "thinking_level")
	if len(got.Options) == 0 {
		t.Fatal("thinking_level shows no choices")
	}
	if !strings.Contains(strings.Join(got.Options, ","), "xhigh") {
		t.Errorf("choices = %v", got.Options)
	}
}

// the theme knob lists every palette, auto first, and a running screen takes a change at once
func TestTheThemeKnobListsEveryPaletteAndIsLive(t *testing.T) {
	fakeHome(t)
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := findSetting(cfg.Catalogue(), "theme")
	if !ok || got.Value != theme.Auto || got.Default != theme.Auto {
		t.Fatalf("theme = %+v; want it listed, auto by default", got)
	}
	if strings.Join(got.Options, ",") != strings.Join(theme.Names(), ",") {
		t.Errorf("choices = %v, want %v", got.Options, theme.Names())
	}
	if !LiveKnobs()["theme"] {
		t.Error("a theme change waits for the next start")
	}
}

func findSetting(list []Setting, key string) (Setting, bool) {
	for _, s := range list {
		if s.Key == key {
			return s, true
		}
	}
	return Setting{}, false
}
