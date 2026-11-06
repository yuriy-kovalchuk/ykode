package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

func TestEveryEventBecomesOneJSONLine(t *testing.T) {
	var buf bytes.Buffer
	w := NewJSONWriter(&buf)

	if got := w.Emit(agent.Event{Kind: agent.ToolStart, Turn: 2, Tool: "read_file", Args: `{"path":"a.go"}`}); got != agent.Refuse {
		t.Fatalf("a script cannot answer a Confirm, got %v", got)
	}
	w.Emit(agent.Event{Kind: agent.Answer, Turn: 2, Text: "42"})
	w.Emit(agent.Event{Kind: agent.TurnEnd, Turn: 2, Stats: agent.Stats{PromptTokens: 900, CachedTokens: 850, FinishReason: "stop"}})
	w.Emit(agent.Event{Kind: agent.Confirm, Tool: "exec_command", Text: "exec_command rm -rf build", Target: []string{"/tmp/outside"}})
	w.Usage(agent.Usage{Turns: 2, PromptTok: 900, CachedTok: 850}, 131072)
	w.Failure(errors.New("chat turn 3: boom"))

	var lines []map[string]any
	dec := json.NewDecoder(&buf)
	for dec.More() {
		var rec map[string]any
		if err := dec.Decode(&rec); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, rec)
	}
	want := []string{"tool_start", "answer", "turn_end", "confirm", "usage", "error"}
	if len(lines) != len(want) {
		t.Fatalf("%d lines, want %d: %v", len(lines), len(want), buf.String())
	}
	for i, kind := range want {
		if lines[i]["kind"] != kind {
			t.Errorf("line %d kind = %v, want %s", i, lines[i]["kind"], kind)
		}
	}
	if lines[0]["tool"] != "read_file" || lines[0]["turn"] != float64(2) {
		t.Errorf("tool_start = %v", lines[0])
	}
	if _, ok := lines[2]["stats"]; !ok {
		t.Errorf("turn_end has no stats: %v", lines[2])
	}
	if targets, ok := lines[3]["target"].([]any); !ok || len(targets) != 1 {
		t.Errorf("confirm = %v, want the outside paths listed", lines[3])
	}
	if lines[4]["context"] != float64(131072) {
		t.Errorf("usage = %v", lines[4])
	}
	if lines[5]["text"] != "chat turn 3: boom" {
		t.Errorf("error line = %v", lines[5])
	}
}
