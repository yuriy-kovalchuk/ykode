package app

import (
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/config"
)

// leaving says what was saved and how to come back for it
func TestLeavingNamesTheConversationItSaved(t *testing.T) {
	cfg := config.Config{Session: "s20260101-101010", SessionFile: "/home/me/.ykode/sessions/s20260101-101010.jsonl"}
	note := resumeNote(cfg, 7)
	for _, want := range []string{"7 messages", "s20260101-101010.jsonl", "--continue s20260101-101010"} {
		if !strings.Contains(note, want) {
			t.Errorf("%q missing from: %q", want, note)
		}
	}
	if note := resumeNote(cfg, 0); note != "" {
		t.Errorf("a run that saved nothing says: %q", note)
	}
}
