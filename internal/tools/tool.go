// Package tools holds the callable surface the agent exposes to the model, and every result is text.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Tool is one callable: Run takes raw JSON arguments and returns text, and Targets reports the paths it touches.
type Tool interface {
	Name() string
	Description() string
	Schema() json.RawMessage // JSON Schema for the "parameters" object
	NeedsConfirm() bool
	Targets(args json.RawMessage) ([]Target, error)
	Run(ctx context.Context, args json.RawMessage) (string, error)
}

// BuiltIn is the tool set ykode ships with, ordered locate, read, change, run.
// Options carries the settings the tools use when the model does not ask for a size of its own.
type Options struct {
	ReadLines     int
	SearchLimit   int
	ExecTimeout   time.Duration
	CommandLogDir string
}

func BuiltIn(root *os.Root, dir string, opts Options) *Registry {
	read := NewReadFile(root, dir)
	read.Lines = opts.ReadLines
	search := NewSearchText(root, dir)
	search.MatchLimit = opts.SearchLimit
	run := NewExec(dir)
	run.Timeout = opts.ExecTimeout
	run.LogDir = opts.CommandLogDir
	return NewRegistry(
		NewListDir(root, dir),
		NewFindFiles(root, dir),
		search,
		read,
		NewWriteFile(root, dir),
		NewEditFile(root, dir),
		run,
	)
}

// Registry keeps tools in registration order so the list the model sees stays stable.
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
	// tools aim under resultBudget so their own note about the cut still fits
	maxResultBytes = 12000
	resultBudget   = maxResultBytes - 512
	noOutput       = "(no output)"
	// markerOverhead leaves room for the "bytes omitted" line itself, which fits a 10 digit count
	markerOverhead = 64
)

// FormatResult keeps a result non-empty and inside maxResultBytes, keeping the head and tail of anything longer.
func FormatResult(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return noOutput
	}
	if len(out) <= maxResultBytes {
		return out
	}
	// the marker costs bytes too, or the result would break the cap it promises
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

// clampLimit maps an unset or bogus limit to the default and cuts anything over the maximum.
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
