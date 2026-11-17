package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// Spec is one tool as the model sees it: its name and what its description costs it.
type Spec struct {
	Name  string
	Bytes int
}

// Specs lists what the model may call, in the order the server receives it.
func (a *Agent) Specs() []Spec {
	var out []Spec
	for _, s := range a.specs() {
		out = append(out, Spec{Name: s.Function.Name, Bytes: specBytes(s)})
	}
	return out
}

// SpecBytes reports how much the tool descriptions cost in every request.
func (a *Agent) SpecBytes() int {
	total := 0
	for _, s := range a.Specs() {
		total += s.Bytes
	}
	return total
}

// ToolsLine answers /tools: what the model can call, and what each description costs it.
func (a *Agent) ToolsLine() string {
	specs := a.Specs()
	var b strings.Builder
	for _, s := range specs {
		fmt.Fprintf(&b, "  %-14s %5d bytes\n", s.Name, s.Bytes)
	}
	fmt.Fprintf(&b, "%d tools, %d bytes in every request", len(specs), a.SpecBytes())
	return b.String()
}

func specBytes(s llm.ToolSpec) int {
	raw, err := json.Marshal(s)
	if err != nil {
		return 0
	}
	return len(raw)
}

// wireSpecBytes is what the tool list costs every request, counted once, not per message dropped
func (a *Agent) wireSpecBytes() int {
	if a.specBytes == 0 {
		for _, s := range a.specs() {
			a.specBytes += len(s.Type) + len(s.Function.Name) + len(s.Function.Description) + len(s.Function.Parameters)
		}
	}
	return a.specBytes
}

// specs describes the tools to the model, in registration order.
func (a *Agent) specs() []llm.ToolSpec {
	if a.tools == nil { // a history-only agent, as the budget tests build one
		return nil
	}
	all := a.tools.All()
	specs := make([]llm.ToolSpec, 0, len(all))
	// the list never changes mid-session: Qwen renders it into the cached system message
	for _, t := range all {
		specs = append(specs, llm.ToolSpec{
			Type:     "function",
			Function: llm.ToolFunction{Name: t.Name(), Description: t.Description(), Parameters: t.Schema()},
		})
	}
	return specs
}
