package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// askedAbout puts a model's question on the screen as a running session would, and returns its channel
func askedAbout(m *model, text string, options ...string) chan string {
	words := make(chan string, 1)
	m.pending = []*question{{text: text, options: options, words: words}}
	m.refresh() // the way a question arriving from the run leaves the screen: the rows are re-measured
	return words
}

// a question and its choices read across rows: the end of a choice is the part that decides it
func TestAChoiceLongerThanTheTerminalIsReadAcrossRows(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 66, 24
	askedAbout(m, "which of these should the sessions list do when you press enter?",
		"continue the conversation here in this run", "print the command to run it in its own terminal")
	plain := stripCodes(m.View())
	for _, want := range []string{"which of these should the", "run it in its own terminal", "1 ", "2 "} {
		if !strings.Contains(plain, want) {
			t.Errorf("the ask lost %q:\n%s", want, plain)
		}
	}
	for _, line := range strings.Split(plain, "\n") {
		if lipgloss.Width(line) > m.width {
			t.Errorf("a row runs past the terminal (%d columns): %q", lipgloss.Width(line), line)
		}
	}
}

// a question longer than the terminal is all of it on screen, since its end is what asks
func TestALongQuestionIsReadAcrossRows(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 80, 24
	long := "the settings screen can stay one column that scrolls, or split into a column of keys and a " +
		"column of values, and the help for a key could sit under the key you have selected or in the " +
		"row at the bottom where the model and the context already live; of those two changes, or " +
		"both, or neither, which do you want?"
	askedAbout(m, long, "the split", "the split and the help line", "neither")
	plain := stripCodes(m.View())
	// the bar down the left of each row is not part of the question, so it comes out before comparing
	flat := strings.Join(strings.Fields(strings.ReplaceAll(plain, askBar, " ")), " ")
	if !strings.Contains(flat, strings.Join(strings.Fields(long), " ")) {
		t.Errorf("the question did not reach the screen whole:\n%s", plain)
	}
	for _, line := range strings.Split(plain, "\n") {
		if lipgloss.Width(line) > m.width {
			t.Errorf("a row runs past the terminal (%d columns): %q", lipgloss.Width(line), line)
		}
	}
}

func TestTheQuestionTheModelAskedShowsItsChoicesNumbered(t *testing.T) {
	m := newTestModel()
	m.width = 90
	askedAbout(m, "which way should the footer follow the branch?", "stat HEAD each 5s", "a file watcher", "leave it")
	screen := stripCodes(m.View())
	for _, want := range []string{
		"which way should the footer follow the branch?",
		"1 stat HEAD each 5s", "2 a file watcher", "3 leave it",
		"esc stops the run",
	} {
		if !strings.Contains(screen, want) {
			t.Errorf("the screen does not say %q:\n%s", want, screen)
		}
	}
	if !strings.Contains(screen, promptMark) {
		t.Errorf("the box went away while the model waited:\n%s", screen)
	}
}

// the number is only how the choice is picked: the model reads the choice itself back
func TestANumberAnswersWithTheChoiceItPointsAt(t *testing.T) {
	m := newTestModel()
	words := askedAbout(m, "which of these?", "the stat poll", "fsnotify")
	typeText(m, "2")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := <-words; got != "fsnotify" {
		t.Errorf("the model was told %q", got)
	}
	if len(m.pending) != 0 {
		t.Error("the question stayed on the screen")
	}
	if m.input.Value() != "" {
		t.Errorf("the box kept the answer: %q", m.input.Value())
	}
}

func TestWordsTypedAnswerInThoseWords(t *testing.T) {
	m := newTestModel()
	words := askedAbout(m, "which file should I start with?")
	typeText(m, "internal/tui/status.go")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := <-words; got != "internal/tui/status.go" {
		t.Errorf("the model was told %q", got)
	}
}

// a number outside the choices is an answer in words, not a silent refusal to answer
func TestANumberNobodyOfferedIsReadAsTyped(t *testing.T) {
	m := newTestModel()
	words := askedAbout(m, "which of these?", "one", "two")
	typeText(m, "9")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := <-words; got != "9" {
		t.Errorf("the model was told %q", got)
	}
}

// nothing typed says there is no answer: the run is told to go on and say what it assumed
func TestAnEmptyAnswerLetsTheRunGoOn(t *testing.T) {
	m := newTestModel()
	words := askedAbout(m, "which of these?", "one", "two")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := <-words; got != "" {
		t.Errorf("the model was told %q", got)
	}
}

// the keys belong to the box while the model waits: a letter is typed, not an approval
func TestTypingWhileTheModelWaitsIsTyping(t *testing.T) {
	m := newTestModel()
	words := askedAbout(m, "shall I rename the file?", "yes", "no")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.input.Value() != "n" {
		t.Errorf("the box holds %q, so a letter answered the question", m.input.Value())
	}
	select {
	case got := <-words:
		t.Fatalf("a single key answered the question with %q", got)
	default:
	}
}

func TestEscAnswersTheQuestionByStoppingTheRun(t *testing.T) {
	m := newTestModel()
	m.running = true // the run is sitting on the question, which is the only way one arrives
	words := askedAbout(m, "which of these?", "one", "two")
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	select {
	case got := <-words:
		if got != "" {
			t.Errorf("stopping answered the question with %q", got)
		}
	default:
		t.Error("the run is still waiting on a question nobody will answer")
	}
}

// the run waits on the screen, and stops waiting when the screen is left or the ask is cancelled
func TestTheHubStopsWaitingWithTheQuestionUnanswered(t *testing.T) {
	hub := NewHub()
	if got := hub.Ask(context.Background(), agent.Event{Kind: agent.Ask, Text: "which of these?"}); got != "" {
		t.Errorf("with no screen to answer, the hub got %q", got)
	}

	hub = NewHub()
	hub.send = func(tea.Msg) {} // a screen that never takes the question up
	ctx, cancel := context.WithCancel(context.Background())
	go cancel()
	if got := hub.Ask(ctx, agent.Event{Kind: agent.Ask, Text: "which of these?"}); got != "" {
		t.Errorf("a cancelled ask left the hub with %q", got)
	}
}

// the wiring test: the question reaches the screen, the typing reaches the model, the run goes on
func TestARunThatAsksWaitsForWhatIsTypedIntoTheBox(t *testing.T) {
	asked := &askedReplies{t: t, question: "which way should the footer follow the branch?",
		options: []string{"stat HEAD each 5s", "a file watcher", "leave it"}, closing: "the stat poll then"}
	server := httptest.NewServer(asked)
	defer server.Close()

	hub := NewHub()
	a := agent.New(llm.NewClient(server.URL+"/v1", ""), tools.NewRegistry(), "test-model",
		agent.Options{MaxTurns: 4, MaxTokens: 64, CtxSize: 8000}, hub)
	in, keys, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer keys.Close()
	defer in.Close()
	screen := &lockedBuffer{}

	done := make(chan error, 1)
	go func() {
		done <- run(context.Background(), a, hub, config.Config{}, screen, tea.WithInput(in), tea.WithOutput(screen))
	}()

	writeKeys(t, keys, "pick the footer way\r")
	waitFor(t, screen, "which way should the footer follow the branch?")
	waitFor(t, screen, "3 leave it") // the choices are numbered the way they are answered
	if asked.answerGiven() != "" {
		t.Errorf("the model was answered %q before anything was typed", asked.answerGiven())
	}

	writeKeys(t, keys, "2\r") // the number picks a choice; the model reads the choice
	waitFor(t, screen, "the stat poll then")
	if got := asked.answerGiven(); !strings.Contains(got, "a file watcher") {
		t.Errorf("the model was handed %q, not the choice the number pointed at", got)
	}

	writeKeys(t, keys, "/exit\r")
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("the session ended with an error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("the session did not end")
	}
}

// askedReplies is a server that asks first, answers once answered, and keeps what came back
type askedReplies struct {
	t        *testing.T
	question string
	options  []string
	closing  string

	mu       sync.Mutex
	seen     int
	answered string // the tool result of the ask call, as the run sent it back
}

func (s *askedReplies) answerGiven() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.answered
}

func (s *askedReplies) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.t.Errorf("unreadable request: %v", err)
	}
	if messages, ok := body["messages"].([]any); ok {
		for _, message := range messages {
			if role, _ := message.(map[string]any)["role"]; role == "tool" {
				text, _ := message.(map[string]any)["content"].(string)
				s.mu.Lock()
				s.answered += text
				s.mu.Unlock()
			}
		}
	}
	args, err := json.Marshal(map[string]any{"question": s.question, "options": s.options})
	if err != nil {
		s.t.Fatal(err)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	s.mu.Lock()
	first := s.seen == 0
	s.seen++
	s.mu.Unlock()
	if first {
		call, err := json.Marshal(map[string]any{"choices": []any{map[string]any{
			"index": 0,
			"delta": map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": "ask1", "type": "function",
				"function": map[string]any{"name": "ask_user", "arguments": string(args)},
			}}},
			"finish_reason": "tool_calls",
		}}})
		if err != nil {
			s.t.Fatal(err)
		}
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", call)
		return
	}
	reply, err := json.Marshal(map[string]any{"choices": []any{map[string]any{
		"index": 0, "delta": map[string]any{"content": s.closing}, "finish_reason": "stop",
	}}})
	if err != nil {
		s.t.Fatal(err)
	}
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", reply)
}

// a short terminal shows the choices that fit, counts the rest, and every number still answers
func TestChoicesAreCutToTheRowsTheScreenHas(t *testing.T) {
	ask := func(height int) (*model, chan string, string) {
		m := newTestModel()
		m.width, m.height = 90, height
		words := askedAbout(m, "which way should the footer follow the branch?",
			"stat HEAD every 5s", "a file watcher", "ask the model each turn", "on demand only",
			"read HEAD once at startup", "leave it as it is")
		return m, words, stripCodes(m.View())
	}

	_, _, screen := ask(24)
	if rows := len(strings.Split(screen, "\n")); rows != 24 {
		t.Errorf("a 24 row terminal drew %d rows:\n%s", rows, screen)
	}
	if !strings.Contains(screen, "6 leave it as it is") {
		t.Errorf("a tall screen did not show all six choices:\n%s", screen)
	}
	if strings.Contains(screen, "more, their numbers") {
		t.Errorf("a tall screen counted choices it had already shown:\n%s", screen)
	}

	short, words, screen := ask(14)
	if rows := len(strings.Split(screen, "\n")); rows != 14 {
		t.Errorf("a 14 row terminal drew %d rows:\n%s", rows, screen)
	}
	if !strings.Contains(screen, "more, their numbers still answer") {
		t.Errorf("the choices that did not fit went uncounted:\n%s", screen)
	}
	if !strings.Contains(screen, promptMark) {
		t.Errorf("the prompt was pushed off the bottom:\n%s", screen)
	}

	typeText(short, "6") // the sixth choice never showed, and its number still means it
	short.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := <-words; got != "leave it as it is" {
		t.Errorf("the model was told %q", got)
	}
	if len(short.pending) != 0 {
		t.Error("the question stayed on the screen")
	}
}
