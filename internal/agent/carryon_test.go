package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// asksSent are the notes ykode added to the conversation in the user's place, in order.
func asksSent(a *Agent) []string {
	var notes []string
	for _, m := range a.history {
		if note, ok := strings.CutPrefix(m.Content, NoteMark); ok && m.Role == "user" {
			notes = append(notes, note)
		}
	}
	return notes
}

// a call the model wrote as text, whole and naming a tool this run has, is run as the call it is: a
// model whose server does not read its markup back would otherwise stop after every second call
func TestACallWrittenAsTextRuns(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "Let me write it.\n<tool_call>\n<function=write_file>\n<parameter=path>\nnote.txt\n</parameter>\n<parameter=content>\nline one\nline two\n</parameter>\n</function>\n</tool_call>", "", nil, "stop"),
		sse("", "<tool_call>\n{\"name\": \"read_file\", \"arguments\": {\"path\": \"note.txt\"}}\n</tool_call>", "", nil, "stop"),
		sse("", "It is written.", "", nil, "stop"),
	}}
	sink := &captureSink{decision: Allow}
	a, done := newAgent(t, stub, Options{MaxTurns: 6, MaxTokens: 256, AssumeYes: true}, sink)
	defer done()
	if err := a.Ask(context.Background(), "write the note"); err != nil {
		t.Fatal(err)
	}
	var results []string
	for _, m := range a.history {
		if m.Role == "tool" {
			results = append(results, m.Name+": "+m.Content)
		}
		if m.Role == "assistant" && hasTextCall(m.Content) {
			t.Errorf("the markup stayed in the conversation: %q", m.Content)
		}
	}
	if len(results) != 2 || !strings.Contains(results[0], "write_file: created note.txt") || !strings.Contains(results[1], "line one\nline two") {
		t.Fatalf("the two calls did not both run with their arguments: %q", results)
	}
	if shown := sink.content.String(); strings.Contains(shown, "<") || !strings.Contains(shown, "Let me write it.") || !strings.Contains(shown, "It is written.") {
		t.Errorf("the screen was handed the markup, or lost the words around it: %q", shown)
	}
	if len(asksSent(a)) != 0 {
		t.Errorf("the model was told off for a call that ran: %q", asksSent(a))
	}
}

// the markup of a call is held back however the stream cuts it up, and words that only look like
// the start of a tag come through
func TestTheMarkupOfACallIsNeverShown(t *testing.T) {
	answer := "a < b and <b>bold</b>, then <tool_call>\n<function=read_file>\n</function>\n</tool_call> done <function=x>y</function>."
	for size := 1; size <= len(answer); size++ {
		var h callHider
		var shown strings.Builder
		for at := 0; at < len(answer); at += size {
			shown.WriteString(h.feed(answer[at:min(at+size, len(answer))]))
		}
		shown.WriteString(h.rest())
		if got, want := shown.String(), "a < b and <b>bold</b>, then  done ."; got != want {
			t.Fatalf("in pieces of %d: shown %q, want %q", size, got, want)
		}
	}
	var open callHider
	if got := open.feed("before <tool_call>{\"name\":") + open.rest(); got != "before " {
		t.Errorf("a call that never closed was shown: %q", got)
	}
}

// a reply cut off at max_tokens is asked to carry on instead of ending the turn there
func TestAReplyCutOffCarriesOn(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("thinking at length", "", "", nil, "length"),
		sse("", "The first half of the", "", nil, "length"),
		sse("", " answer.", "", nil, "stop"),
	}}
	a, done := newAgent(t, stub, Options{MaxTurns: 8, MaxTokens: 64}, &captureSink{})
	defer done()
	if err := a.Ask(context.Background(), "explain it"); err != nil {
		t.Fatal(err)
	}
	notes := asksSent(a)
	if len(stub.requests) != 3 || len(notes) != 2 || !strings.Contains(notes[0], "still thinking") || !strings.Contains(notes[1], "cut off by the token limit") {
		t.Fatalf("%d requests, notes %q; want the thinking that ran out and the answer that was cut each asked to go on", len(stub.requests), notes)
	}
}

// a model that says what it will do next and then stops is asked once to do it; one that asks the
// user something, or has answered, is left alone
func TestAReplyThatStopsHalfwayIsAskedToCarryOn(t *testing.T) {
	a := New(nil, nil, "test-model", Options{Plan: tools.NewPlan()}, nil)
	for said, halfway := range map[string]bool{
		"The config is read. Now let me update the tests.":   false, // the last line is the whole reply: it opens with what was found
		"The config is read.\n\nNow let me update the tests": true,
		"Let me check the build next":                        true,
		"Next, I'll run the suite.":                          true,
		"Here is what I will change:":                        true,
		"All three files are updated and the tests pass.":    false,
		"Which of the two do you want?":                      false,
		"Let me know if you want the other one changed too.": false,
	} {
		if got := a.stoppedHalfway(said); got != halfway {
			t.Errorf("%q: read as halfway=%v, want %v", said, got, halfway)
		}
	}
	a.opts.Plan.Restore([]tools.PlanStep{{Text: "read", Status: tools.PlanDone}, {Text: "write", Status: tools.PlanActive}})
	if !a.stoppedHalfway("The reading is done.") {
		t.Error("a reply that leaves a step of the plan open reads as an answer")
	}
	if a.stoppedHalfway("Should I overwrite the file?") {
		t.Error("a question to the user was read as a pause")
	}
	report := "## Verdict: not ready\n\nTwo UI bugs.\n\nWant me to fix #1 and #2 (both ~2 lines)? I'd leave the toolchain bump as a separate commit."
	if a.stoppedHalfway(report) {
		t.Error("a report that ends by asking the user something was read as a pause")
	}
	if !a.stoppedHalfway("Is the build green? Yes.\n\nNow let me update the tests.") {
		t.Error("a question answered in an earlier paragraph made the pause read as a question")
	}
}

func TestStoppingHalfwayCostsOneNudge(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "read_file", []string{`{"path":"a.go"}`}, "tool_calls"),
		sse("", "The files are listed.\n\nNow let me read the first one", "", nil, "stop"),
		sse("", "Still about to.\n\nLet me read it", "", nil, "stop"),
	}}
	a, done := newAgent(t, stub, Options{MaxTurns: 8, MaxTokens: 64}, &captureSink{})
	defer done()
	if err := a.Ask(context.Background(), "look around"); err != nil {
		t.Fatal(err)
	}
	if notes := asksSent(a); len(notes) != 1 || !strings.Contains(notes[0], "stopped before the work was finished") || len(stub.requests) != 3 {
		t.Errorf("notes %q over %d requests; want one nudge, then the reply taken as it is", notes, len(stub.requests))
	}
}

// a model that works on without touching its plan is reminded of it on a result it is about to read
func TestAStalePlanIsRemindedOf(t *testing.T) {
	plan := tools.NewPlan()
	scripts := []string{sse("", "", "plan", []string{`{"steps":[{"text":"look","status":"active"},{"text":"answer"}]}`}, "tool_calls")}
	for i := 0; i < planStaleCalls; i++ {
		scripts = append(scripts, sse("", "", "read_file", []string{`{"path":"f` + string(rune('a'+i)) + `.go"}`}, "tool_calls"))
	}
	scripts = append(scripts, sse("", "Do you want more?", "", nil, "stop"))
	stub := &stubServer{scripts: scripts}
	a, done := newAgent(t, stub, Options{MaxTurns: 20, MaxTokens: 64, Plan: plan}, &captureSink{})
	defer done()
	a.tools = tools.NewRegistry(append(a.tools.All(), plan)...)
	if err := a.Ask(context.Background(), "look around"); err != nil {
		t.Fatal(err)
	}
	reminded := 0
	for i, m := range a.history {
		if m.Role == "tool" && strings.Contains(m.Content, "the plan has not been updated") {
			reminded++
			if i != len(a.history)-2 {
				t.Errorf("the reminder is on message %d of %d, not on the newest result", i, len(a.history))
			}
		}
	}
	if reminded != 1 {
		t.Errorf("the model was reminded %d times in %d calls", reminded, planStaleCalls)
	}
}

// out of rounds, the model is asked where the work stands, in words it cannot take for the user's,
// and calls it writes anyway are not kept as its answer
func TestOutOfRoundsAsksWhereTheWorkStands(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "read_file", []string{`{"path":"a.go"}`}, "tool_calls"),
		sse("", "Done so far: the listing.\n<tool_call>\n<function=read_file>\n</function>\n</tool_call>", "", nil, "stop"),
	}}
	sink := &captureSink{}
	a, done := newAgent(t, stub, Options{MaxTurns: 1, MaxTokens: 64}, sink)
	defer done()
	if err := a.Ask(context.Background(), "look around"); err != nil {
		t.Fatal(err)
	}
	notes := asksSent(a)
	if len(notes) != 1 || !strings.Contains(notes[0], "where the work stands") {
		t.Fatalf("the model was told %q", notes)
	}
	last := a.history[len(a.history)-1]
	if last.Content != "Done so far: the listing." || len(last.ToolCalls) != 0 {
		t.Errorf("the reply kept calls that never ran: %q %v", last.Content, last.ToolCalls)
	}
	told := false
	for _, ev := range sink.events {
		told = told || (ev.Kind == Notice && strings.Contains(ev.Text, "continue"))
	}
	if !told {
		t.Error("nothing told the user how to carry on")
	}
}

// a server started with no flags has a window of four thousand tokens and ykode's default margin is
// as many: the reserve gives way, and a turn still completes
func TestATurnCompletesInASmallWindow(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("", "done", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{CtxSize: 4096, SafetyMargin: 4096, MaxTurns: 4}, &captureSink{})
	defer done()
	if err := a.Ask(context.Background(), "say something"); err != nil {
		t.Fatalf("a window of 4096 with a margin of 4096 cannot answer: %v", err)
	}
	if got := stub.lastRequest(t)["max_tokens"].(float64); got < minAnswerTok {
		t.Errorf("the reply was left %v tokens", got)
	}
}

// whatever the drop leaves, the request that follows it can go: the mark it drops to and the check a
// request makes are one rule
func TestWhatDroppingLeavesCanBeSent(t *testing.T) {
	for _, window := range []int{2048, 4096, 8192, 32768} {
		a := New(nil, nil, "test-model", Options{CtxSize: window, SafetyMargin: 4096}, nil)
		a.history = append(a.history, llm.Message{Role: "user", Content: "read everything"})
		for i := 0; i < 40; i++ {
			id := fmt.Sprintf("c%d", i)
			a.history = append(a.history,
				llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: id, Type: "function", Function: llm.FunctionCall{Name: "read_file", Arguments: json.RawMessage(`{"path":"a.go"}`)}}}},
				llm.Message{Role: "tool", ToolCallID: id, Name: "read_file", Content: strings.Repeat("a line of the file that was read\n", 60)})
		}
		a.makeRoom()
		if _, err := a.request(); err != nil {
			t.Errorf("window %d: dropped to, then refused: %v", window, err)
		}
	}
}

// a server that says the request does not fit is believed: room is made and the turn asked again
func TestAnOverflowIsRecoveredFromOnce(t *testing.T) {
	asked := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if asked++; asked == 1 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":{"code":400,"message":"the request exceeds the available context size, try increasing it","type":"exceed_context_size_error"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse("", "it fits now", "", nil, "stop"))
	}))
	defer server.Close()
	sink := &captureSink{}
	a := New(llm.NewClient(server.URL+"/v1", ""), tools.NewRegistry(), "test-model", Options{CtxSize: 8000, MaxTurns: 4}, sink)
	before := a.meter.bytesPerTok
	if err := a.Ask(context.Background(), "say something"); err != nil {
		t.Fatalf("the overflow ended the turn: %v", err)
	}
	if asked != 2 || a.meter.bytesPerTok >= before || !strings.Contains(sink.content.String(), "it fits now") {
		t.Errorf("%d requests, ratio %.2f -> %.2f, answer %q", asked, before, a.meter.bytesPerTok, sink.content.String())
	}
}

// a conversation that was folded before it was saved does not fold again the moment it comes back
func TestAResumedFoldWaitsForNewWork(t *testing.T) {
	a := New(nil, nil, "test-model", Options{}, nil)
	fresh := New(nil, nil, "test-model", Options{}, nil)
	fresh.Resume([]llm.Message{{Role: "user", Content: "hello"}})
	a.Resume([]llm.Message{{Role: "user", Content: FoldMark + "\n## Goal\nfinish it"}, {Role: "user", Content: "carry on"}})
	if !fresh.foldOverdue() || a.foldOverdue() {
		t.Errorf("overdue: never folded %v, folded before it was saved %v", fresh.foldOverdue(), a.foldOverdue())
	}
}
