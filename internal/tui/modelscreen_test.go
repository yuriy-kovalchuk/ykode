package tui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// modelScreenModel is a session whose server lists two models and has one of them in use.
func modelScreenModel(t *testing.T, listing string) *model {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, listing)
	}))
	t.Cleanup(srv.Close)
	a := agent.New(llm.NewClient(srv.URL+"/v1", ""), nil, "qwen-small", agent.Options{}, nil)
	m := newModel(context.Background(), a, NewHub(), config.Config{})
	m.Update(tea.WindowSizeMsg{Width: 92, Height: 24})
	return m
}

const twoModels = `{"data":[{"id":"qwen-small","meta":{"n_ctx":32768}},{"id":"qwen-big","meta":{"n_ctx":131072}}]}`

// openModels opens the box at once and returns the command that waits for the server
func openModels(t *testing.T, m *model) string {
	t.Helper()
	m.input.SetValue("/model")
	_, cmd := m.Update(enter)
	if m.picker == nil {
		t.Fatal("/model did not open the list")
	}
	if cmd == nil {
		t.Fatal("/model did not ask the server what it offers")
	}
	m.Update(cmd())
	return stripCodes(m.View())
}

// a bare /model shows the server's models with their windows, marking the one in use
func TestModelListsWhatTheServerOffers(t *testing.T) {
	m := modelScreenModel(t, twoModels)
	view := openModels(t, m)

	for _, want := range []string{"ykode · model", "2 models on this server", "qwen-small", "qwen-big",
		"131k window", "this is the model this run is using"} {
		if !strings.Contains(view, want) {
			t.Errorf("the model list lacks %q:\n%s", want, view)
		}
	}
	if row := rowHolding(view, "qwen-small"); !strings.Contains(row, "*") {
		t.Errorf("the model in use is not marked out: %q", row)
	}
}

// enter switches the run to the model under the cursor, and the switch is said out loud
func TestEnterSwitchesTheRunToAnotherModel(t *testing.T) {
	m := modelScreenModel(t, twoModels)
	openModels(t, m)
	m.Update(down)
	m.Update(enter)

	if m.picker != nil {
		t.Error("the box stayed up after the model was switched")
	}
	if got := m.agent.Model(); got != "qwen-big" {
		t.Errorf("the run is on %q, not the model that was picked", got)
	}
	if view := stripCodes(m.View()); !strings.Contains(view, "model: qwen-big") {
		t.Errorf("the switch was not reported:\n%s", view)
	}
}

// the model already in use has nowhere to switch to, and the box says so instead of closing
func TestTheModelInUseIsNotSwitchedTo(t *testing.T) {
	m := modelScreenModel(t, twoModels)
	openModels(t, m)
	m.Update(enter)

	if m.picker == nil {
		t.Fatal("the box closed on the model this run is already using")
	}
	if got := m.agent.Model(); got != "qwen-small" {
		t.Errorf("the run moved to %q without being asked to", got)
	}
	if view := stripCodes(m.View()); !strings.Contains(view, "this is the model this run is using") {
		t.Errorf("the box does not say why nothing happened:\n%s", view)
	}
}

// the box opens before the server answers, and says that is what it is doing
func TestModelOpensWhileTheServerIsStillAnswering(t *testing.T) {
	m := modelScreenModel(t, twoModels)
	m.input.SetValue("/model")
	_, cmd := m.Update(enter)
	if cmd == nil {
		t.Fatal("/model did not ask the server what it offers")
	}
	if view := stripCodes(m.View()); !strings.Contains(view, "asking the server") {
		t.Errorf("the empty box does not say the server has not answered:\n%s", view)
	}
}

// a server that will not list its models is said so, in the box, rather than a blank list
func TestModelSaysWhenTheServerWillNotList(t *testing.T) {
	m := modelScreenModel(t, `not json`)
	view := openModels(t, m)

	if !strings.Contains(view, "did not list its models") {
		t.Errorf("a server that would not answer was not reported:\n%s", view)
	}
}

// /model with an argument keeps the text answer, because it can name a model the server does not list
func TestModelWithAnArgumentStillAnswersInText(t *testing.T) {
	m := modelScreenModel(t, twoModels)
	m.input.SetValue("/model qwen-big")
	m.Update(enter)

	if m.picker != nil {
		t.Error("/model with an argument opened the list instead of switching")
	}
	if got := m.agent.Model(); got != "qwen-big" {
		t.Errorf("the run is on %q", got)
	}
}
