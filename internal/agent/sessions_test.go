package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/session"
)

// /sessions names the conversations that can be resumed, marking this project's and this run's,
// because an id is a timestamp nobody picks out of a list
func TestSessionsNamesWhatCanBeResumed(t *testing.T) {
	dir := t.TempDir()
	sessions := filepath.Join(dir, "sessions")
	here := session.New(sessions, "s20260101-120000.000", dir)
	elsewhere := session.New(sessions, "s20260101-110000.000", "/somewhere/else")
	for _, saved := range []struct {
		log *session.Log
		ask string
	}{{here, "this project's ask"}, {elsewhere, "another project's ask"}} {
		if err := saved.log.Append(llm.Message{Role: "user", Content: saved.ask}); err != nil {
			t.Fatal(err)
		}
		if err := saved.log.Append(llm.Message{Role: "assistant", Content: "done"}); err != nil {
			t.Fatal(err)
		}
	}
	a, done := newStoringAgent(t, &stubServer{}, Options{Session: here, Root: dir}, &captureSink{}, dir)
	defer done()
	a.add(llm.Message{Role: "user", Content: "what this run is doing"})

	text, _, err := a.Command(context.Background(), "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	own := sessionsRow(text, "s20260101-120000.000")
	if !strings.Contains(own, "*") || !strings.Contains(own, "(this run)") {
		t.Errorf("this run's conversation is not marked out: %q", own)
	}
	if other := sessionsRow(text, "s20260101-110000.000"); strings.Contains(other, "*") {
		t.Errorf("another project's conversation is marked as ours: %q", other)
	}
	for _, want := range []string{"--continue", "this project's ask", "another project's ask"} {
		if !strings.Contains(text, want) {
			t.Errorf("the listing is missing %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "120000") > strings.Index(text, "110000") {
		t.Error("the newest conversation is not first")
	}
}

// a run that saves nothing says so instead of an empty list
func TestSessionsWithoutASavedConversation(t *testing.T) {
	dir := t.TempDir()
	a, done := newStoringAgent(t, &stubServer{}, Options{}, &captureSink{}, dir)
	defer done()

	text, _, err := a.Command(context.Background(), "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "no conversation") {
		t.Errorf("sessions off answered: %q", text)
	}
}

func sessionsRow(listing, id string) string {
	for _, line := range strings.Split(listing, "\n") {
		if strings.Contains(line, id) {
			return line
		}
	}
	return ""
}
