// Package tools holds the callable surface the agent exposes to the model.
// In-project file access goes through one *os.Root; paths outside the project
// root work only for a single call the user approved. Every result is text.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Tool is one callable. Run receives the model's raw JSON arguments; decode
// them into a small struct and return text. Errors are reported back to the
// model as result text, so they should say what to fix. Targets reports the
// paths a call would touch, so the caller can confirm access before running it.
type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage // JSON Schema for the "parameters" object
	NeedsConfirm() bool
	Targets(args json.RawMessage) ([]Target, error)
	Run(ctx context.Context, args json.RawMessage) (string, error)
}

// BuiltIn is the tool set ykode ships with, in the order the model sees it:
// locate, read, change, run.
func BuiltIn(root *os.Root, dir string) *Registry {
	return NewRegistry(
		NewListDir(root, dir),
		NewFindFiles(root, dir),
		NewSearchText(root, dir),
		NewReadFile(root, dir),
		NewWriteFile(root, dir),
		NewEditFile(root, dir),
		NewExec(dir),
	)
}

// Registry holds the tools in registration order, which keeps the tool list
// the model sees stable from run to run.
type Registry struct {
	byName  map[string]Tool
	ordered []Tool
}

func NewRegistry(all ...Tool) *Registry {
	r := &Registry{byName: make(map[string]Tool, len(all))}
	for _, t := range all {
		if _, dup := r.byName[t.Name()]; dup {
			continue
		}
		r.byName[t.Name()] = t
		r.ordered = append(r.ordered, t)
	}
	return r
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.byName[name]
	return t, ok
}

func (r *Registry) All() []Tool { return r.ordered }

func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.ordered))
	for _, t := range r.ordered {
		names = append(names, t.Name())
	}
	return names
}

const (
	// maxResultBytes is the hard cap on anything the model is shown. Tools aim
	// under resultBudget so their own explanation of the cut still fits.
	maxResultBytes = 12000
	resultBudget   = maxResultBytes - 512
	noOutput       = "(no output)"
	// markerOverhead reserves room for the "bytes omitted" line itself, which is
	// long enough for a 10 digit count.
	markerOverhead = 64
)

// FormatResult makes a tool result safe to append to the conversation: never
// empty, and at most maxResultBytes, keeping the head and the tail of anything
// longer (long logs usually matter at both ends).
func FormatResult(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return noOutput
	}
	if len(out) <= maxResultBytes {
		return out
	}
	// The marker costs bytes too; without the reservation the result could
	// exceed the cap it promises the agent.
	keep := (maxResultBytes - markerOverhead) / 2
	head, tail := cutUTF8(out, keep), cutUTF8Tail(out, keep)
	return fmt.Sprintf("%s\n\n[... %d bytes omitted ...]\n\n%s", head, len(out)-len(head)-len(tail), tail)
}

// cutUTF8 returns the first n bytes of s, never splitting a rune.
func cutUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !isRuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// cutUTF8Tail returns the last n bytes of s, never splitting a rune.
func cutUTF8Tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	start := len(s) - n
	for start < len(s) && !isRuneStart(s[start]) {
		start++
	}
	return s[start:]
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// clamp keeps a model-supplied count inside a supported range.
func clamp(v, lo, hi int) int { return min(max(v, lo), hi) }

// clampLimit resolves a limit the model supplied: unset or nonsense means the
// default, and anything above the supported maximum is cut down to it.
func clampLimit(limit, def, upper int) int {
	if limit <= 0 {
		return def
	}
	return clamp(limit, 1, upper)
}

// toolError names the tool and the path or command it failed on.
func toolError(tool, target string, err error) error {
	return fmt.Errorf("%s %q: %w", tool, target, err)
}

// decodeArgs unmarshals the model's arguments, naming the failure usefully.
func decodeArgs(raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("bad arguments: %w", err)
	}
	return nil
}
