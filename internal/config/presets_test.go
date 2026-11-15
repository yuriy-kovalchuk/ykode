package config

import (
	"path/filepath"
	"testing"
)

func TestSamplingIsSetPerModelAndThinkingMode(t *testing.T) {
	user := fakeHome(t)
	writeSettings(t, filepath.Join(user, "settings.yaml"), `{"models": {"qwen": {
		"thinking": {"temperature": 0.6, "top_p": 0.95, "top_k": 20, "min_p": 0, "presence_penalty": 1.5},
		"no_think": {"temperature": 0.7, "top_p": 0.8}
	}}}`)
	cfg, err := Load(nil)
	if err != nil {
		t.Fatal(err)
	}
	thinking, noThink := cfg.SamplingFor("qwen")
	if *thinking.Temperature != 0.6 || *thinking.TopK != 20 || *thinking.MinP != 0 || *thinking.PresencePenalty != 1.5 {
		t.Errorf("thinking sampling = %+v", thinking)
	}
	if *noThink.Temperature != 0.7 || noThink.TopK != nil {
		t.Errorf("no-think sampling = %+v", noThink)
	}
	if other, _ := cfg.SamplingFor("other"); other.Temperature != nil {
		t.Error("another model got qwen's sampling")
	}
}
