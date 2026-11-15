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

// A settings file can recommend numbers per model. A switch moved the window and the model id and kept
// sending the model it had left: the card for the model actually being asked for was never looked up.
func TestASwitchPicksUpTheModelItIsSwitchingTo(t *testing.T) {
	a := agentOnAServerWithModels(t, Options{
		CtxSize:  1000,
		Thinking: llm.Sampling{Temperature: sp(0.2)},
		Cards: map[string]llm.ModelCard{
			"small": {Thinking: llm.Sampling{Temperature: sp(0.9)}, SendReasoning: true},
		},
	})
	before := a.opts.SendReasoning
	a.Command(context.Background(), "/model small")
	if got := a.opts.Thinking.Temperature; got == nil || *got != 0.9 {
		t.Errorf("after /model small the sampling sent is %+v, want the temperature that model's card recommends", a.opts.Thinking)
	}
	if a.opts.SendReasoning == before {
		t.Errorf("send_reasoning for the new model was not taken: still %v", a.opts.SendReasoning)
	}
	// a model nothing was written about keeps what is in use: nothing is known about it, and the
	// server's own default is not necessarily what had been working
	a.Command(context.Background(), "/model big")
	if got := a.opts.Thinking.Temperature; got == nil || *got != 0.9 {
		t.Errorf("a model with no card lost the numbers in use: %+v", a.opts.Thinking)
	}
}

// A model the server does not list has no window of its own. The run keeps the one it had and says so,
// rather than switching the budget off without a word.
func TestAModelOffTheListKeepsTheWindowAndSaysSo(t *testing.T) {
	a := agentOnAServerWithModels(t, Options{CtxSize: 1000})
	text, _, _ := a.Command(context.Background(), "/model proxy-loaded")
	if a.CtxSize() != 1000 {
		t.Errorf("window after an unlisted model: %d, want the 1000 that was in use", a.CtxSize())
	}
	if !strings.Contains(text, "keeping the") {
		t.Errorf("the line did not say the window is a carry-over: %q", text)
	}
}

func sp(v float64) *float64 { return &v }
