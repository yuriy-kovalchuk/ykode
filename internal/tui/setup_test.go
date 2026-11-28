package tui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/theme"
)

// a probe that answers like a server with two models, and records what it was asked
type fakeServer struct {
	asked   []string
	refuses bool
}

func (f *fakeServer) probe(_ context.Context, url string) (string, []llm.ModelInfo, error) {
	f.asked = append(f.asked, url)
	if f.refuses {
		return url, nil, errors.New("connection refused")
	}
	return url, []llm.ModelInfo{{ID: "qwen-small", CtxSize: 32768}, {ID: "qwen-big", CtxSize: 131072}}, nil
}

func press(m *setupModel, keys ...tea.KeyPressMsg) {
	for _, k := range keys {
		_, cmd := m.Update(k)
		// only enter's command is the probe; other keys return the cursor's blink timer
		if cmd != nil && k.Code == tea.KeyEnter {
			if msg, ok := cmd().(probeDoneMsg); ok {
				m.Update(msg)
			}
		}
	}
}

var (
	enter = tea.KeyPressMsg{Code: tea.KeyEnter}
	down  = tea.KeyPressMsg{Code: tea.KeyDown}
	esc   = tea.KeyPressMsg{Code: tea.KeyEscape}
	space = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
)

func typed(s string) tea.KeyPressMsg { return typedKeys(s) }

// the whole first run: a URL, enter, a model, enter - and the answer is what gets saved
func TestSetupConnectsAndPicksAModel(t *testing.T) {
	server := &fakeServer{}
	m := newSetup(SetupStart{BaseURL: "http://gpu.local:8080/v1"}, server.probe)

	press(m, enter)
	if m.step != stepModel {
		t.Fatalf("after connecting, the screen is at step %d, want the model list (err %q)", m.step, m.err)
	}
	if view := m.render(); !strings.Contains(view, "qwen-small") || !strings.Contains(view, "131k context") {
		t.Errorf("the models and their windows are not listed:\n%s", view)
	}
	press(m, down, enter)
	if !m.done {
		t.Fatal("choosing a model did not finish the setup")
	}
	if m.result.BaseURL != "http://gpu.local:8080/v1" || m.result.Model != "qwen-big" || m.result.CtxSize != 131072 {
		t.Errorf("result = %+v, want the server and the second model", m.result)
	}
}

// a server that does not answer keeps the form, says why, and lets the URL be fixed
func TestSetupSaysWhyAServerDidNotAnswer(t *testing.T) {
	server := &fakeServer{refuses: true}
	m := newSetup(SetupStart{BaseURL: "http://nowhere:1/v1"}, server.probe)
	press(m, enter)
	if m.step != stepServer || !strings.Contains(m.render(), "connection refused") {
		t.Fatalf("a refused connection is not explained on the form:\n%s", m.render())
	}
	server.refuses = false
	press(m, enter)
	if m.step != stepModel {
		t.Error("a second try after the server came up did not get through")
	}
}

// the URL is cleaned up the way people type it: no scheme, a trailing slash
func TestSetupCleansUpATypedURL(t *testing.T) {
	server := &fakeServer{}
	m := newSetup(SetupStart{}, server.probe)
	press(m, typed("gpu.local:8080/v1/"), enter)
	if len(server.asked) != 1 || server.asked[0] != "http://gpu.local:8080/v1" {
		t.Errorf("the server was asked at %v, want http://gpu.local:8080/v1", server.asked)
	}
	m = newSetup(SetupStart{BaseURL: ""}, server.probe)
	press(m, enter)
	if !strings.Contains(m.err, "URL is needed") {
		t.Errorf("an empty URL was not caught: %q", m.err)
	}
}

// a screen opened because the server went away says so first
func TestSetupShowsWhyItOpened(t *testing.T) {
	m := newSetup(SetupStart{BaseURL: "http://old/v1", Problem: "cannot reach server at http://old/v1"}, (&fakeServer{}).probe)
	if !strings.Contains(m.render(), "cannot reach server") {
		t.Errorf("the reason the setup opened is not shown:\n%s", m.render())
	}
}

// esc on the form leaves without a result; on the model list it goes back to the form
func TestSetupEscGoesBackThenLeaves(t *testing.T) {
	m := newSetup(SetupStart{BaseURL: "http://gpu/v1"}, (&fakeServer{}).probe)
	press(m, enter, esc)
	if m.step != stepServer || m.done {
		t.Fatal("esc on the model list did not go back to the form")
	}
	_, cmd := m.Update(esc)
	if cmd == nil || m.done {
		t.Error("esc on the form did not leave, or left with a result")
	}
}

// llama-server serves the OpenAI routes under /v1; a URL typed without it still connects
func TestTheProbeAddsTheV1PeopleLeaveOff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"qwen","meta":{"n_ctx":4096}}]}`)
	}))
	defer srv.Close()

	found, models, err := probeServer(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if found != srv.URL+"/v1" || len(models) != 1 || models[0].ID != "qwen" {
		t.Errorf("found %q with %v, want %s/v1 and the one model", found, models, srv.URL)
	}
	if _, _, err := probeServer(context.Background(), srv.URL+"/nothing"); err == nil {
		t.Error("a URL nothing answers at was reported as working")
	}
}

// run on its own the server screen is in the chosen theme, and auto still follows the terminal
func TestTheServerScreenRunOnItsOwnTakesTheTheme(t *testing.T) {
	t.Cleanup(func() { usePalette(theme.For(true)) })
	m := newSetup(SetupStart{Theme: "nord"}, nil)
	m.ownPalette()
	nord, _ := theme.Named("nord")
	if v := m.View(); v.BackgroundColor == nil {
		t.Error("the screen left the terminal its own ground under nord")
	}
	if codeOf(setupTitle) != codeOf(lipgloss.NewStyle().Foreground(colour(nord.Cyan)).Bold(true)) {
		t.Error("the title is not in nord's cyan")
	}
	m.Update(tea.BackgroundColorMsg{Color: color.White})
	if m.painted.Name != "nord" {
		t.Errorf("a light terminal swapped nord for %s", m.painted.Name)
	}

	auto := newSetup(SetupStart{Theme: theme.Auto}, nil)
	auto.ownPalette()
	auto.Update(tea.BackgroundColorMsg{Color: color.White})
	if auto.painted.Name != "ykode-light" || auto.View().BackgroundColor != nil {
		t.Errorf("auto on a light terminal drew %s", auto.painted.Name)
	}
}

// a URL pasted on the server step lands in the URL box, so it can be connected to
func TestSetupTakesAPastedURL(t *testing.T) {
	server := &fakeServer{}
	m := newSetup(SetupStart{}, server.probe)
	m.Update(tea.PasteMsg{Content: "http://gpu.local:8080/v1"})
	press(m, enter)
	if len(server.asked) != 1 || server.asked[0] != "http://gpu.local:8080/v1" {
		t.Errorf("the server was asked at %v, want the pasted URL", server.asked)
	}
}
