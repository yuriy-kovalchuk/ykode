package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

func longPaste(lines int) string {
	rows := make([]string, lines)
	for i := range rows {
		rows[i] = fmt.Sprintf("log line %d: something happened here", i+1)
	}
	return strings.Join(rows, "\n")
}

// a long paste is one token in the box, so the box stays as quick as it is with nothing in it
func TestALongPasteIsOneTokenInTheBox(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.input.InsertString("why does this fail? ")
	m.Update(tea.PasteMsg{Content: longPaste(400)})
	if got := m.input.Value(); got != "why does this fail? [Pasted text #1 +400 lines]" {
		t.Errorf("the box holds %d bytes: %q", len(got), got[:min(len(got), 80)])
	}
}

// a paste the box can show whole is typed as it is: a token would hide what is about to be sent
func TestAShortPasteIsTypedAsItIs(t *testing.T) {
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.Update(tea.PasteMsg{Content: "go test ./...\ngo vet ./..."})
	if got := m.input.Value(); got != "go test ./...\ngo vet ./..." {
		t.Errorf("the box holds %q", got)
	}
}

// one long line is held too, and its token says its size since it has no lines to count
func TestOneLongLineIsHeldWithItsSize(t *testing.T) {
	var p pastes
	token, held := p.hold(strings.Repeat("x", 5000))
	if !held || !strings.HasPrefix(token, "[Pasted text #1, ") {
		t.Errorf("held %v as %q", held, token)
	}
}

// each token goes back to its own paste; one the session never handed out is left as typed
func TestEveryTokenExpandsToItsOwnPaste(t *testing.T) {
	var p pastes
	first, _ := p.hold(longPaste(20))
	second, _ := p.hold(strings.Repeat("a\r\n", 11) + "b")
	got := p.expand("compare " + first + " with " + second + " and [Pasted text #9 +2 lines]")
	want := "compare " + longPaste(20) + " with " + strings.Repeat("a\n", 11) + "b and [Pasted text #9 +2 lines]"
	if got != want {
		t.Errorf("expanded to %q", got)
	}
}

// the model reads the whole paste, while the ask's card in the transcript keeps the token
func TestASentPasteReachesTheModelWholeAndTheCardKeepsTheToken(t *testing.T) {
	got := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		if last := req.Messages[len(req.Messages)-1]; last.Role == "user" {
			select {
			case got <- fmt.Sprint(last.Content):
			default:
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	a := agent.New(llm.NewClient(server.URL+"/v1"), tools.NewRegistry(), "test-model",
		agent.Options{MaxTurns: 2, CtxSize: 8000}, nil)
	m := newModel(context.Background(), a, NewHub(), config.Config{})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	m.input.InsertString("explain ")
	m.Update(tea.PasteMsg{Content: longPaste(50)})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	select {
	case sent := <-got:
		if !strings.Contains(sent, "log line 50: something happened here") || strings.Contains(sent, "[Pasted text") {
			t.Errorf("the model was sent the token, not the paste: %.120q", sent)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the ask never reached the server")
	}
	if card := stripCodes(strings.Join(m.log.rows(100), "\n")); !strings.Contains(card, "explain [Pasted text #1 +50 lines]") || strings.Contains(card, "log line 2:") {
		t.Errorf("the card shows the whole paste, or lost its token:\n%s", card)
	}
	if m.history[len(m.history)-1] != "explain [Pasted text #1 +50 lines]" {
		t.Errorf("history kept %q, want the token, which still sends the paste", m.history[len(m.history)-1])
	}
}

// ctrl+v with text on the clipboard comes back as the textarea's own string type, and is held the same
func TestAClipboardPasteIsHeldLikeATerminalOne(t *testing.T) {
	type clipboardText string // the shape textarea.Paste hands over
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.pasted(pastedMsg{err: errNoPicture, text: clipboardText(longPaste(30))})
	if got := m.input.Value(); got != "[Pasted text #1 +30 lines]" {
		t.Errorf("the box holds %.80q", got)
	}
}

// a paste while the settings list is browsed goes nowhere: the box it would land in is hidden under it
func TestAPasteNeverReachesTheBoxBehindTheSettings(t *testing.T) {
	settingsHome(t, "")
	m := newTestModel()
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	openRow(t, m, "max_turns")
	m.Update(tea.PasteMsg{Content: "hidden text"})
	m.Update(pastedMsg{err: errNoPicture, text: tea.PasteMsg{Content: "from the clipboard"}})
	if got := m.input.Value(); got != "" {
		t.Errorf("the box behind the screen took the paste: %q", got)
	}
}

// a paste while a value is typed on the settings screen goes into that value
func TestAPasteOnTheSettingsScreenTypesTheValue(t *testing.T) {
	settingsHome(t, "")
	m := newTestModel()
	openRow(t, m, "max_turns")
	m.Update(enter) // opens the typing box
	m.screen.menu.input.SetValue("")
	m.Update(tea.PasteMsg{Content: "40"})
	if got := m.screen.menu.input.Value(); got != "40" || m.input.Value() != "" {
		t.Errorf("the value holds %q and the box %q", got, m.input.Value())
	}
}

// a paste over a list narrows it as typing would, and leaves the box under it alone
func TestAPasteNarrowsAList(t *testing.T) {
	m := newTestModel()
	m.Update(window92)
	m.input.SetValue("/help")
	m.submit()
	m.Update(tea.PasteMsg{Content: "compact"})
	if view := stripCodes(m.render()); strings.Contains(view, "/sessions") || m.input.Value() != "" {
		t.Errorf("the paste did not narrow the list (box %q):\n%s", m.input.Value(), view)
	}
}
