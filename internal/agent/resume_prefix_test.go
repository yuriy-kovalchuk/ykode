package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/session"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// rawStub records the exact bytes of every request body, because a cache compares bytes, not meaning.
// It answers with scripted streams.
type rawStub struct {
	scripts []string
	bodies  [][]byte
}

func (s *rawStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	s.bodies = append(s.bodies, body)
	w.Header().Set("Content-Type", "text/event-stream")
	i := min(len(s.bodies)-1, len(s.scripts)-1)
	_, _ = w.Write([]byte(s.scripts[i]))
}

func (s *rawStub) agent(t *testing.T, dir string, log *session.Log) (*Agent, func()) {
	srv := httptest.NewServer(s)
	root, _ := os.OpenRoot(dir)
	store := tools.NewResultStore(filepath.Join(dir, "saved"))
	registry := tools.BuiltIn(root, dir, tools.Options{Store: store, MaxResultBytes: 4000})
	a := New(llm.NewClient(srv.URL+"/v1", ""), registry, "test-model",
		Options{MaxTurns: 6, MaxTokens: 256, CtxSize: 100000, Root: dir, Session: log, AssumeYes: true}, &captureSink{decision: Allow})
	a.opts.Store = store
	return a, func() { srv.Close(); root.Close() }
}

type reqShape struct {
	Tools    json.RawMessage   `json:"tools"`
	Messages []json.RawMessage `json:"messages"`
}

func shape(t *testing.T, body []byte) reqShape {
	var r reqShape
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAResumedRequestRepeatsTheLiveOneByteForByte(t *testing.T) {
	dir := t.TempDir()
	writeLines(t, dir, "a.txt", 5)
	call := sse("let me look", "", "read_file", []string{`{"path": "a.txt",  "offset":1}`}, "tool_calls") // a model's own spacing
	live := &rawStub{scripts: []string{call, sse("", "it has five lines", "", nil, "stop"), sse("", "second answer", "", nil, "stop")}}
	log := session.New(dir, "s1", dir)
	a, done := live.agent(t, dir, log)
	defer done()
	if err := a.Ask(context.Background(), "read a.txt"); err != nil {
		t.Fatal(err)
	}
	back, err := session.Load(filepath.Join(dir, "s1.jsonl"), dir) // what a resume would read: the log as the first ask left it
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Ask(context.Background(), "and the second thing?"); err != nil {
		t.Fatal(err)
	}
	liveReq := shape(t, live.bodies[2]) // the second ask, as the live session sent it

	resumed := &rawStub{scripts: []string{sse("", "second answer", "", nil, "stop")}}
	b, done2 := resumed.agent(t, dir, nil)
	defer done2()
	b.Resume(back)
	if err := b.Ask(context.Background(), "and the second thing?"); err != nil {
		t.Fatal(err)
	}
	resReq := shape(t, resumed.bodies[0])

	// the server's cache matches from the first token, so a resume that words anything differently,
	// even an argument's spacing, makes the server read the whole conversation again
	if string(liveReq.Tools) != string(resReq.Tools) {
		t.Error("the tool definitions differ after a resume")
	}
	if len(liveReq.Messages) != len(resReq.Messages) {
		t.Fatalf("the live request holds %d messages, the resumed one %d", len(liveReq.Messages), len(resReq.Messages))
	}
	for i := range liveReq.Messages {
		if string(liveReq.Messages[i]) != string(resReq.Messages[i]) {
			t.Fatalf("message %d differs after a resume:\n live:    %.300s\n resumed: %.300s", i, liveReq.Messages[i], resReq.Messages[i])
		}
	}
}

// crowded is a session whose window fills while it works: every read is a few kilobytes and the
// window holds a few of them, so the run shortens what aged and trims what still does not fit.
func crowded(t *testing.T, dir string, stub *rawStub, log *session.Log) (*Agent, func()) {
	a, done := stub.agent(t, dir, log)
	a.opts.CtxSize, a.opts.MaxTokens = 4000, 128
	return a, done
}

func asJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// what a run does to its conversation to make it fit - shortening an old result, dropping a call - is
// in the saved conversation too: a run that comes back sends what the first one last sent, so the
// server still has it, instead of the conversation as it was before any of that
func TestAResumeOfACrowdedSessionSendsWhatTheSessionLastSent(t *testing.T) {
	dir := t.TempDir()
	scripts := []string{}
	for _, name := range []string{"a.txt", "b.txt", "c.txt", "d.txt", "e.txt"} {
		writeLines(t, dir, name, 120)
		scripts = append(scripts, sse("", "", "read_file", []string{`{"path":"` + name + `"}`}, "tool_calls"))
	}
	scripts = append(scripts, sse("", "all read", "", nil, "stop"))
	live := &rawStub{scripts: scripts}
	log := session.New(dir, "s1", dir)
	a, done := crowded(t, dir, live, log)
	defer done()
	if err := a.Ask(context.Background(), "read the five files"); err != nil {
		t.Fatal(err)
	}
	shortened := false
	for _, m := range a.history {
		shortened = shortened || strings.HasPrefix(m.Content, shrinkMark)
	}
	if !shortened {
		t.Fatal("the session never shortened anything, so it tests nothing")
	}

	saved, err := session.LoadAll(filepath.Join(dir, "s1.jsonl"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := asJSON(t, saved.Messages), asJSON(t, a.history[1:]); got != want {
		t.Fatalf("the saved conversation is not the one being sent:\n saved: %.400s\n sent:  %.400s", got, want)
	}

	if err := a.Ask(context.Background(), "and now?"); err != nil {
		t.Fatal(err)
	}
	liveReq := shape(t, live.bodies[len(live.bodies)-1])

	resumed := &rawStub{scripts: []string{sse("", "all read", "", nil, "stop")}}
	b, done2 := crowded(t, dir, resumed, nil)
	defer done2()
	b.Learned(saved.BytesPerToken)
	b.Resume(saved.Messages)
	if err := b.Ask(context.Background(), "and now?"); err != nil {
		t.Fatal(err)
	}
	resReq := shape(t, resumed.bodies[len(resumed.bodies)-1])
	if len(liveReq.Messages) != len(resReq.Messages) {
		t.Fatalf("the live request holds %d messages, the resumed one %d", len(liveReq.Messages), len(resReq.Messages))
	}
	for i := range liveReq.Messages {
		if string(liveReq.Messages[i]) != string(resReq.Messages[i]) {
			t.Fatalf("message %d differs after a resume:\n live:    %.300s\n resumed: %.300s", i, liveReq.Messages[i], resReq.Messages[i])
		}
	}
}

// what the run learned about how its server counts comes back with the conversation
func TestTheLearnedRatioIsSavedWithTheConversation(t *testing.T) {
	dir := t.TempDir()
	log := session.New(dir, "s1", dir)
	if err := log.Append(llm.Message{Role: "user", Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := log.Learned(4.2); err != nil {
		t.Fatal(err)
	}
	saved, err := session.LoadAll(filepath.Join(dir, "s1.jsonl"), dir)
	if err != nil || saved.BytesPerToken != 4.2 || len(saved.Messages) != 1 {
		t.Fatalf("read back %+v, %v", saved, err)
	}
	a := New(nil, nil, "test-model", Options{}, nil)
	a.Learned(saved.BytesPerToken)
	if a.meter.bytesPerTok != 4.2 {
		t.Errorf("the run measures with %.1f bytes a token, not what was learned", a.meter.bytesPerTok)
	}
}
