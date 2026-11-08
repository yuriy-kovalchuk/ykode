package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// a call being written arrives a token at a time; the JSON stream carries it once, whole, at tool_start
func TestTheJSONStreamLeavesOutCallsBeingWritten(t *testing.T) {
	var out bytes.Buffer
	w := NewJSONWriter(&out)
	w.Emit(agent.Event{Kind: agent.ToolDraft, Tool: "write_file", Text: `{"path":`})
	w.Emit(agent.Event{Kind: agent.ToolStart, Tool: "write_file", Args: `{"path":"a.go"}`})
	if strings.Contains(out.String(), "tool_draft") {
		t.Errorf("the stream carries the call's fragments:\n%s", out.String())
	}
	if !strings.Contains(out.String(), `"kind":"tool_start"`) {
		t.Errorf("the whole call is missing:\n%s", out.String())
	}
}
