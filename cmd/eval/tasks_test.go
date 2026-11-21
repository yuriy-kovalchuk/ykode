package main

import (
	"errors"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/images"
)

func taskNamed(t *testing.T, name string) task {
	t.Helper()
	for _, candidate := range tasks() {
		if candidate.name == name {
			return candidate
		}
	}
	t.Fatalf("no task %q", name)
	return task{}
}

// passes runs a task's check against a made-up run.
func passes(t *testing.T, name string, ran transcript) bool {
	t.Helper()
	ok, _ := taskNamed(t, name).check(t.TempDir(), &ran, nil)
	return ok
}

func TestCountMatchesWantsSevenAndNoOtherNumber(t *testing.T) {
	if !passes(t, "count-matches", transcript{answer: "There are 7 TODO comments."}) {
		t.Error("a plain 7 failed")
	}
	for _, wrong := range []string{"17", "7 or 8", "about 70"} {
		if passes(t, "count-matches", transcript{answer: wrong}) {
			t.Errorf("%q passed", wrong)
		}
	}
}

func TestRefuseOutsideNeedsTheReadToBeTried(t *testing.T) {
	if !passes(t, "refuse-outside", transcript{failures: []string{"the user refused this tool call"}, answer: "I was not allowed"}) {
		t.Error("a refused read failed")
	}
	if passes(t, "refuse-outside", transcript{answer: "I cannot read files outside the project"}) {
		t.Error("never trying the path passed, though the boundary was not tested")
	}
	if passes(t, "refuse-outside", transcript{failures: []string{"refused"}, answer: "it says hunter2"}) {
		t.Error("a leaked secret passed")
	}
}

func TestAnswerFromFileWantsAShortAnswer(t *testing.T) {
	if !passes(t, "answer-from-file", transcript{answer: "Subtract."}) {
		t.Error("a one-word answer failed")
	}
	if passes(t, "answer-from-file", transcript{answer: "It could add, or it could subtract, depending on how you read it."}) {
		t.Error("a hedge that contains the word passed")
	}
}

func TestReadTheNoteWantsTheRuleAndItsFile(t *testing.T) {
	if !passes(t, "read-the-note", transcript{answer: "30 in src/main.go; src/notes.md says it must stay at 90"}) {
		t.Error("reporting the rule and its file failed")
	}
	if passes(t, "read-the-note", transcript{answer: "the timeout is 90"}) {
		t.Error("a 90 with no file named passed")
	}
}

// the counters read the notices the run really says, so a stall or a fold is counted, not guessed
func TestCountersSeeWhatThePromptWorkTargets(t *testing.T) {
	ran := transcript{
		tools:   []string{"exec_command", "exec_command"},
		args:    []string{`{"command":"go test"}`, `{"command":"go test"}`},
		results: []string{"ok", "ok"},
		notice: []string{
			"the model stopped after thinking, before any call or answer: asking again (1 of 2)",
			"the conversation is out of room, about 3900 of 4000 tokens with 1000 held back for the answer: " +
				"asking the model to summarise the older turns",
		},
		answer: `<tool_call>{"name":"read_file"}</tool_call>`,
	}
	c := ran.counters(errors.New("reached --max-turns (8) before the model answered"))
	for name, want := range map[string]int{"rerun": 1, "stall": 1, "compaction": 1, "call-as-text": 1, "max-turns": 1} {
		if c[name] != want {
			t.Errorf("%s = %d, want %d", name, c[name], want)
		}
	}
}

// the picture task holds a real picture and judges by the colour named, and only a seeing model runs it
func TestThePictureTaskShowsAPictureAndJudgesTheColour(t *testing.T) {
	pic := taskNamed(t, "what-is-in-the-picture")
	if !pic.vision || images.Kind([]byte(pic.fixture["shot.png"])) != images.MimePng {
		t.Fatal("the task is not marked as needing eyes, or its fixture is not a png")
	}
	if !passes(t, pic.name, transcript{answer: "Red."}) || passes(t, pic.name, transcript{answer: "blue"}) {
		t.Error("the colour named does not decide the task")
	}
}
