package app

import (
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

func TestEveryRunGetsTheSamePlanAndSettings(t *testing.T) {
	cfg := config.Config{MaxTurns: 9, MaxTokens: 512, SafetyMargin: 99, ThinkingLevel: "medium",
		ReadLines: 40, Root: "/work/p", Allow: []string{"edit_file"}}
	toolOpts, agentOpts := Options(cfg, "m", 8000)

	if toolOpts.Plan == nil || toolOpts.Plan != agentOpts.Plan {
		t.Error("the screen reads the plan the plan tool writes, so they must be one list")
	}
	if toolOpts.ReadLines != 40 {
		t.Errorf("tool options = %+v", toolOpts)
	}
	if agentOpts.MaxTurns != 9 || agentOpts.SafetyMargin != 99 || agentOpts.CtxSize != 8000 ||
		agentOpts.Root != "/work/p" || len(agentOpts.Allow) != 1 {
		t.Errorf("agent options = %+v", agentOpts)
	}
	if toolOpts.MaxResultBytes != agentOpts.MaxResult || agentOpts.MaxResult != tools.ResultBytesFor(8000) {
		t.Error("the tools and the agent disagree on the result budget")
	}
}

// A window named on the command line is a request about this server, not about whichever model gets
// picked next: unmarked, a model switch replaces the number the user typed with the model's own.
func TestAWindowNamedByTheFlagIsMarkedAsPinned(t *testing.T) {
	_, pinned := Options(config.Config{CtxSize: 8000}, "m", 8000)
	if !pinned.CtxPinned {
		t.Error("--ctx 8000 did not mark the window as fixed")
	}

	// the window read from the server is not the user's number, so a model switch may replace it
	_, chosen := Options(config.Config{}, "m", 32000)
	if chosen.CtxPinned {
		t.Error("a window the server volunteered was marked as one the user asked for")
	}
}
