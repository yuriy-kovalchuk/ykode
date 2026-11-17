package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// a server that lists one model
func serverWith(t *testing.T, id string) *llm.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"` + id + `","meta":{"n_ctx":262144}}]}`))
	}))
	t.Cleanup(srv.Close)
	return llm.NewClient(srv.URL, "")
}

// a model pinned in settings.yaml belongs to its endpoint: another endpoint picks its own, and says so
func TestAPinnedModelTheServerDoesNotHaveIsReplacedByItsOwn(t *testing.T) {
	homeDir, root := scratchHome(t)
	if err := os.WriteFile(filepath.Join(homeDir, "settings.yaml"), []byte("model: old-qwen\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load([]string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	model, note, err := pickModel(context.Background(), serverWith(t, "strata-model"), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if model.ID != "strata-model" || model.CtxSize != 262144 {
		t.Errorf("the session would ask for %q (%d), want the server's model with its window", model.ID, model.CtxSize)
	}
	if !strings.Contains(note, "old-qwen") || !strings.Contains(note, "strata-model") {
		t.Errorf("the note does not name both models: %q", note)
	}
}

// a model the user named on the command line is a request: it is kept, with a warning
func TestAModelNamedByAFlagIsKeptEvenWhenTheServerDoesNotListIt(t *testing.T) {
	_, root := scratchHome(t)
	cfg, err := config.Load([]string{"--root", root, "--model", "proxy-loads-it"})
	if err != nil {
		t.Fatal(err)
	}
	model, note, err := pickModel(context.Background(), serverWith(t, "other"), cfg)
	if err != nil || model.ID != "proxy-loads-it" || !strings.Contains(note, "not listed") {
		t.Errorf("model %q, note %q, err %v", model.ID, note, err)
	}
}

// the model the server has, and nothing to say about it
func TestAModelTheServerHasNeedsNoNote(t *testing.T) {
	_, root := scratchHome(t)
	cfg, err := config.Load([]string{"--root", root, "--model", "here"})
	if err != nil {
		t.Fatal(err)
	}
	if model, note, err := pickModel(context.Background(), serverWith(t, "here"), cfg); err != nil || note != "" || model.ID != "here" {
		t.Errorf("model %q, note %q, err %v", model.ID, note, err)
	}
}
