package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// every setting says what it is now, what it would be with nothing set, and who set it
func TestEachSettingSaysWhatItIsNowAndWhence(t *testing.T) {
	user := fakeHome(t)
	writeSettings(t, filepath.Join(user, "settings.yaml"), "mouse: true\npermissions:\n  allow:\n    - exec:go test\n")
	t.Setenv("LOCAL_LLM_URL", "http://from-env:8080/v1")

	cfg, err := Load([]string{"--max-turns", "9"})
	if err != nil {
		t.Fatal(err)
	}
	list := cfg.Catalogue()
	if len(list) < 15 {
		t.Fatalf("the list holds %d settings; the file has more to show", len(list))
	}

	want := []Setting{
		{Key: "mouse", Value: "on", Default: "off", From: FromFile},
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

// a token in the settings is reported as set, never spelled out
func TestTheTokenStaysOutOfTheList(t *testing.T) {
	t.Setenv("LOCAL_LLM_API_KEY", "sk-secret-token")
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	for _, s := range cfg.Catalogue() {
		out.WriteString(s.Key + " " + s.Value + " ")
	}
	if strings.Contains(out.String(), "sk-secret-token") {
		t.Errorf("the list repeated the token: %s", out.String())
	}
	got, _ := findSetting(cfg.Catalogue(), "api_key")
	if got.Value == "" || got.From != FromEnv {
		t.Errorf("api_key = %q from %s, want it marked as set from env", got.Value, got.From)
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

func findSetting(list []Setting, key string) (Setting, bool) {
	for _, s := range list {
		if s.Key == key {
			return s, true
		}
	}
	return Setting{}, false
}
