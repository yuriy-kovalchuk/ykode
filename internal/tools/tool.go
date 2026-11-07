// Package tools holds the callable surface the agent exposes to the model, and every result is text.
package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	ReadLines      int
	SearchLimit    int
	ExecTimeout    time.Duration
	CommandLogDir  string
	MaxResultBytes int
	Store          ResultStore
	Undo           *UndoStack // where write and edit leave a snapshot for /undo
	Sandbox        bool       // confine exec_command to the project, offline
}

func BuiltIn(root *os.Root, dir string, opts Options) *Registry {
	find := NewFindFiles(root, dir)
	search := NewSearchText(root, dir)
	read := NewReadFile(root, dir)
	write := NewWriteFile(root, dir)
	edit := NewEditFile(root, dir)
	for _, scope := range []*scope{&find.scope, &search.scope, &read.scope, &write.scope, &edit.scope} {
		scope.budget = opts.MaxResultBytes
	}
	write.undo = opts.Undo
	edit.undo = opts.Undo
	search.MatchLimit = opts.SearchLimit
	read.Lines = opts.ReadLines

	run := NewExec(dir)
	run.Timeout = opts.ExecTimeout
	run.LogDir = opts.CommandLogDir
	run.MaxBytes = opts.MaxResultBytes
	run.Sandbox = opts.Sandbox

	return NewRegistry(find, search, read, write, edit, run)
}

// Detail is what a tool can say about its last result beyond the text, for the audit log.
type Detail struct {
	ExitCode *int   // for a command: what it exited with
	TimedOut bool   // for a command: it was killed for taking too long
	Diff     string // for a change: what it did to the file
	LogPath  string // where the full output went when the result was cut
}

// Shown is one result a tool handed the model: the call it answered and the text it was given.
type Shown struct {
	CallID string // the run's result id, e.g. r7
	Text   string
}

// Forgetter is a tool that remembers what it already showed the model, so a repeat costs one line
// instead of the whole text again. The agent says when the conversation stops holding that text -
// shortened, trimmed, dropped, or reset - and then the next ask must bring the text back.
type Forgetter interface {
	Forget(shown Shown)
	ForgetAll()
}

// resultKey fingerprints a result, which is what a tool keys its memory of showing it on.
func resultKey(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// Detailed is a tool that keeps a little structure about the result it just returned, for the
// audit log. Only tools that never run beside each other may do this: writes, edits and
// commands go one at a time, while the read-only tools can overlap and so keep nothing of the
// sort — a pinned invariant in registry_test.go.
type Detailed interface {
	LastDetail() Detail
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
	// defaultResultBytes caps one tool result when the context window is unknown
	defaultResultBytes = 12000
	// tools aim under their cap so their own note about the cut still fits
	budgetSlack = 512
	noOutput    = "(no output)"
	// markerOverhead leaves room for the "bytes omitted" line itself, which fits a 10 digit count
	markerOverhead = 64
)

// ResultBytesFor scales one tool result with the window it shares: about a tenth of it in tokens,
// clamped so a small window still gets a usable result and a huge one does not drown the model.
func ResultBytesFor(ctxTokens int) int {
	if ctxTokens <= 0 {
		return defaultResultBytes
	}
	return clamp(ctxTokens/10, 1200, 6000) * 4 // four bytes per token is a generous estimate
}

// FormatResult keeps a result non-empty and inside maxBytes, keeping the head and tail of anything longer.
func FormatResult(out string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = defaultResultBytes
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return noOutput
	}
	if len(out) <= maxBytes {
		return out
	}
	// the marker costs bytes too, or the result would break the cap it promises
	keep := (maxBytes - markerOverhead) / 2
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
