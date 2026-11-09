package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

func planCall(id, steps string) llm.Message {
	return llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: id, Type: "function",
		Function: llm.FunctionCall{Name: "plan", Arguments: []byte(steps)}}}}
}

func planResult(id, content string) llm.Message {
	return llm.Message{Role: "tool", Name: "plan", ToolCallID: id, Content: content}
}

// a resumed conversation brings the newest plan call that ran back, so the screen shows it again
func TestAResumedConversationGetsItsPlanBack(t *testing.T) {
	a := &Agent{opts: Options{Plan: tools.NewPlan()}}
	a.restorePlan([]llm.Message{
		{Role: "system", Content: "s"},
		planCall("c1", `{"steps":[{"text":"old","status":"done"}]}`), planResult("c1", "plan 1/1"),
		planCall("c2", `{"steps":[{"text":"read","status":"done"},{"text":"build","status":"active"}]}`), planResult("c2", "plan 1/2"),
		planCall("c3", `{"steps":[{"text":"refused"}]}`), planResult("c3", "error: plan: no step to keep"),
	})
	steps := a.Plan()
	if len(steps) != 2 || steps[1].Text != "build" || steps[1].Status != tools.PlanActive {
		t.Errorf("the plan came back as %+v, want the newest call that ran", steps)
	}
}

// a fold carries the plan in its recap, the recap's summary does not include it, and a resume reads it
func TestAFoldKeepsThePlanAndAResumeFindsIt(t *testing.T) {
	a := &Agent{opts: Options{Plan: tools.NewPlan()}}
	a.opts.Plan.Restore([]tools.PlanStep{{Text: "read", Status: tools.PlanDone}, {Text: "build", Status: tools.PlanActive}})
	block := a.planBlock()
	if !strings.Contains(block, "<plan>\nplan 1/2\n[x] read\n[*] build\n</plan>") {
		t.Fatalf("the block does not carry the plan:\n%s", block)
	}
	recap := llm.Message{Role: "user", Content: FoldMark + "\n## Goal\nx" + block}
	if text, ok := recapOf(recap); !ok || strings.Contains(text, "[x]") || !strings.Contains(text, "## Goal") {
		t.Errorf("the summary handed back for updating carries the plan: %q", text)
	}
	b := &Agent{opts: Options{Plan: tools.NewPlan()}}
	b.restorePlan([]llm.Message{recap})
	if got := b.Plan(); len(got) != 2 || got[0].Status != tools.PlanDone || got[1].Text != "build" {
		t.Errorf("a resume found %+v in the recap", got)
	}
	if (&Agent{opts: Options{Plan: tools.NewPlan()}}).planBlock() != "" {
		t.Error("an empty plan left a block in the recap")
	}
}

// /plan prints the steps as the model last wrote them, and /reset forgets them
func TestThePlanCommandShowsThePlanAndResetClearsIt(t *testing.T) {
	a, done := newAgent(t, &stubServer{}, Options{MaxTurns: 2, Plan: tools.NewPlan()}, &captureSink{})
	defer done()
	if text, _, _ := a.Command(context.Background(), "/plan"); !strings.Contains(text, "nothing planned") {
		t.Errorf("/plan with no plan: %q", text)
	}
	a.opts.Plan.Restore([]tools.PlanStep{{Text: "build it", Status: tools.PlanActive}})
	if text, _, _ := a.Command(context.Background(), "/plan"); !strings.Contains(text, "[*] build it") {
		t.Errorf("/plan did not show the step: %q", text)
	}
	a.Command(context.Background(), "/reset")
	if len(a.Plan()) != 0 {
		t.Error("/reset left the plan")
	}
}

// a plan only the screen and the model read does not count as a change to the project
func TestAPlanCallDoesNotResetWhatTheRunRemembersOfTheProject(t *testing.T) {
	if changesProject("plan") || changesProject("read_file") || !changesProject("write_file") || !changesProject("exec_command") {
		t.Error("changesProject does not tell the plan and the reads from the writes and the commands")
	}
}
