package app

import (
	"strings"
	"testing"
)

// leaving says what was saved and how to come back for it
func TestLeavingNamesTheConversationItSaved(t *testing.T) {
	file, root := "/home/me/.ykode/sessions/s20260101-101010.jsonl", "/home/me/project"
	note := resumeNote(file, root, 7)
	for _, want := range []string{"7 messages", "s20260101-101010.jsonl",
		"ykode --continue s20260101-101010 --root /home/me/project"} {
		if !strings.Contains(note, want) {
			t.Errorf("%q missing from: %q", want, note)
		}
	}
	if note := resumeNote(file, root, 0); note != "" {
		t.Errorf("a run that saved nothing says: %q", note)
	}
}

// the line is meant to be pasted, so a project path with a space has to be quoted
func TestAResumeCommandQuotesAProjectPathWithASpace(t *testing.T) {
	if note := resumeNote("/s/s1.jsonl", "/home/me/My Project", 1); !strings.Contains(note, `--root "/home/me/My Project"`) {
		t.Errorf("the command breaks when pasted with a space in the path:\n%s", note)
	}
}
