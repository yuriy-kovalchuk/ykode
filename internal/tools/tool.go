// Package tools holds the callable surface the agent exposes to the model, and every result is text.
package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
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
	Plan           *Plan      // the steps of a long task, which the model writes and the screen shows
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

	plan := opts.Plan
	if plan == nil {
		plan = NewPlan()
	}

	return NewRegistry(find, search, read, write, edit, run, plan)
}

// Detail is what a tool can say about its last result beyond the text, for the audit log.
type Detail struct {
	ExitCode *int   // for a command: what it exited with
	TimedOut bool   // for a command: it was killed for taking too long
	Diff     string // for a change: what it did to the file
	Change   string // for a change: all of it, for a screen. The model is sent the short one above
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
