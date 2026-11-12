package app

import (
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/config"
)

// leaving says what was saved and how to come back for it
func TestLeavingNamesTheConversationItSaved(t *testing.T) {
	cfg := config.Config{Session: "s20260101-101010", SessionFile: "/home/me/.ykode/sessions/s20260101-101010.jsonl",
		Root: "/home/me/project"}
	note := resumeNote(cfg, 7)
	for _, want := range []string{"7 messages", "s20260101-101010.jsonl",
		"ykode --continue s20260101-101010 --root /home/me/project"} {
		if !strings.Contains(note, want) {
			t.Errorf("%q missing from: %q", want, note)
		}
	}
	if note := resumeNote(cfg, 0); note != "" {
		t.Errorf("a run that saved nothing says: %q", note)
	}
}

// the line is meant to be pasted, so a project path with a space has to be quoted
func TestAResumeCommandQuotesAProjectPathWithASpace(t *testing.T) {
	cfg := config.Config{Session: "s1", SessionFile: "/s/s1.jsonl", Root: "/home/me/My Project"}
	if note := resumeNote(cfg, 1); !strings.Contains(note, `--root "/home/me/My Project"`) {
		t.Errorf("the command breaks when pasted with a space in the path:\n%s", note)
	}
}
