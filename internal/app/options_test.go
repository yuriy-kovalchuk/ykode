package app

import (
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

func TestEveryRunGetsTheSameStoreUndoAndSettings(t *testing.T) {
	cfg := config.Config{MaxTurns: 9, MaxTokens: 512, SafetyMargin: 99, Think: true, Sandbox: true,
		ReadLines: 40, SearchLimit: 7, Root: "/work/p", Allow: []string{"edit_file"}}
	store := tools.NewResultStore(t.TempDir())
	undo := &tools.UndoStack{}

	toolOpts, agentOpts := Options(cfg, "m", 8000, store, undo)

	if !toolOpts.Store.Saved() || !agentOpts.Store.Saved() {
		t.Error("the tools and the agent must share the result store, or recall is never offered")
	}
	if toolOpts.Undo != undo || agentOpts.Undo != undo {
		t.Error("the agent's /undo must read the stack the tools write")
	}
	if !toolOpts.Sandbox || toolOpts.ReadLines != 40 || toolOpts.SearchLimit != 7 {
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
