package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
