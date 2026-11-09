package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// a session that was saved mid-work: an ask, a call and its answer, and what came of it
func savedConversation() []llm.Message {
	return savedConversationIn("")
}

func savedConversationIn(dir string) []llm.Message {
	args, _ := json.Marshal(map[string]string{"path": dir + "src/parser.go"})
	return []llm.Message{
		{Role: "user", Content: "make parse skip blank lines"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1", Function: llm.FunctionCall{Name: "read_file", Arguments: args}}}},
		{Role: "tool", Name: "read_file", ToolCallID: "c1", Content: "12: func parse(line string) {"},
		{Role: "assistant", Content: "done: the blank line check is in place"},
	}
}

// a call carried over names its file from where the project starts, not from the root of the disk
func TestACarriedOverCallNamesItsFileFromTheProject(t *testing.T) {
	a := agent.New(nil, nil, "test-model", agent.Options{Root: "/home/dev/playground"}, nil)
	a.Resume(savedConversationIn("/home/dev/playground/"))
	m := newModel(context.Background(), a, NewHub(), config.Config{Session: "s20260101-120000.000"})

	var row string
	for _, line := range strings.Split(stripCodes(m.View()), "\n") {
		if strings.Contains(line, "read_file") {
			row = line
		}
	}
	if !strings.Contains(row, "read_file src/parser.go") {
		t.Errorf("the row does not name the file from the project: %q", row)
	}
	if strings.Contains(row, "/home/dev/") {
		t.Errorf("the row writes the whole path: %q", row)
	}
}

// coming back to a conversation shows what was already said: the model has it, and an empty screen
// says otherwise
func TestAResumedConversationIsOnTheScreen(t *testing.T) {
	a := agent.New(nil, nil, "test-model", agent.Options{}, nil)
	a.Resume(savedConversation())
	m := newModel(context.Background(), a, NewHub(), config.Config{Session: "s20260101-120000.000"})

	out := stripCodes(m.View())
	for _, want := range []string{"make parse skip blank lines", "done: the blank line check is in place", "read_file", "src/parser.go"} {
		if !strings.Contains(out, want) {
			t.Errorf("the resumed session does not show %q:\n%s", want, out)
		}
	}
}

// coming back also remembers the asks, so the earlier prompts are still walkable
func TestAReplayKeepsTheEarlierAsks(t *testing.T) {
	a := agent.New(nil, nil, "test-model", agent.Options{}, nil)
	a.Resume(savedConversation())
	m := newModel(context.Background(), a, NewHub(), config.Config{})

	if len(m.history) == 0 || m.history[len(m.history)-1] != "make parse skip blank lines" {
		t.Errorf("the prompts of the saved conversation are not there to walk back to: %q", m.history)
	}
}

// a replayed ask does not pretend to know the hour it was asked
func TestAReplayedAskShowsNoClock(t *testing.T) {
	var log transcript
	log.replay([]llm.Message{{Role: "user", Content: "an ask from an earlier run"}})
	if out := stripCodes(log.render(80)); strings.Contains(out, "00:00") {
		t.Errorf("a replayed ask is stamped with an hour it never had:\n%s", out)
	}
}

// a fold comes back as a fold, not as something the user is supposed to have typed
func TestAReplayedFoldIsDrawnAsAFold(t *testing.T) {
	var log transcript
	log.replay([]llm.Message{{Role: "user", Content: agent.FoldMark + "\nthe older turns were folded into this"}})

	out := stripCodes(log.render(80))
	if !strings.Contains(out, "the older turns, folded into this") || !strings.Contains(out, "folded into this") {
		t.Errorf("the recap of a saved fold is not drawn as a recap:\n%s", out)
	}
	if strings.Contains(out, userMark) {
		t.Errorf("the recap is drawn as a prompt:\n%s", out)
	}
}

// a result whose call the trimming took away is still shown, marked as such
func TestAResultWhoseCallIsGoneIsStillShown(t *testing.T) {
	var log transcript
	log.replay([]llm.Message{{Role: "tool", Name: "exec_command", Content: "the output of a call nobody sees"}})

	out := stripCodes(log.render(80))
	if !strings.Contains(out, "exec_command") || !strings.Contains(out, "the output of a call nobody sees") {
		t.Errorf("a result on its own is not drawn:\n%s", out)
	}
}

// the turns that were never a turn of the model's own are not replayed at all
func TestNothingButTheConversationIsReplayed(t *testing.T) {
	var log transcript
	log.replay([]llm.Message{
		{Role: "system", Content: "the system prompt, which was never asked"},
		{Role: "user", Content: "the ask that was"},
	})
	out := stripCodes(log.render(80))
	if strings.Contains(out, "never asked") {
		t.Errorf("the system prompt was replayed:\n%s", out)
	}
	if !strings.Contains(out, "the ask that was") {
		t.Errorf("the ask was not replayed:\n%s", out)
	}
}

// what the model thought was saved with what it said, so a resumed session shows it again
func TestAResumedConversationShowsWhatTheModelThought(t *testing.T) {
	var log transcript
	log.replay([]llm.Message{
		{Role: "system", Content: "s"}, {Role: "user", Content: "fix it"},
		{Role: "assistant", Content: "done", Reasoning: "the bug is in a.go"},
	})
	if out := stripCodes(log.render(80)); !strings.Contains(out, "the bug is in a.go") {
		t.Errorf("the thinking did not come back:\n%s", out)
	}
}
