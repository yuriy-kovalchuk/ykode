package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

func agentOnAServerWithModels(t *testing.T, opts Options) *Agent {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"small","meta":{"n_ctx":32768}},{"id":"big","meta":{"n_ctx":262144}}]}`))
	}))
	t.Cleanup(srv.Close)
	return New(llm.NewClient(srv.URL, ""), tools.NewRegistry(), "big", opts, &captureSink{})
}

// /model on its own lists what the server offers, numbered, with the one in use marked
func TestModelListsTheServersModels(t *testing.T) {
	a := agentOnAServerWithModels(t, Options{CtxSize: 262144})
	text, _, _ := a.Command(context.Background(), "/model")
	for _, want := range []string{"2 model(s)", "1   small", "32k window", "2 * big", "262k window", "/model ID or /model NUMBER"} {
		if !strings.Contains(text, want) {
			t.Errorf("the list lacks %q:\n%s", want, text)
		}
	}
}

// an id or a number switches, and the model brings its own window unless --ctx fixed one
func TestModelSwitchesByIdOrNumberAndTakesItsWindow(t *testing.T) {
	a := agentOnAServerWithModels(t, Options{CtxSize: 262144})
	if _, _, err := a.Command(context.Background(), "/model 1"); err != nil {
		t.Fatal(err)
	}
	if a.Model() != "small" || a.CtxSize() != 32768 {
		t.Errorf("after /model 1: %s with %d tokens, want small with 32768", a.Model(), a.CtxSize())
	}
	a.Command(context.Background(), "/model big")
	if a.Model() != "big" || a.CtxSize() != 262144 {
		t.Errorf("after /model big: %s with %d", a.Model(), a.CtxSize())
	}
	pinned := agentOnAServerWithModels(t, Options{CtxSize: 50000, CtxPinned: true})
	pinned.Command(context.Background(), "/model small")
	if pinned.CtxSize() != 50000 {
		t.Errorf("--ctx was overridden by the model's window: %d", pinned.CtxSize())
	}
}

// a model the server does not list is still accepted, with a word about it
func TestModelAcceptsOneTheServerDoesNotList(t *testing.T) {
	a := agentOnAServerWithModels(t, Options{CtxSize: 1000})
	text, _, _ := a.Command(context.Background(), "/model proxy-loaded")
	if a.Model() != "proxy-loaded" || !strings.Contains(text, "does not list it") || a.CtxSize() != 1000 {
		t.Errorf("model %q, window %d, text %q", a.Model(), a.CtxSize(), text)
	}
}
