// Package tools holds the callable surface the agent exposes to the model, and every result is text.
package tools

import (
	"context"
	"encoding/json"
	"os"
	"time"
)

// Tool is one callable: Run takes raw JSON arguments and returns text, and Targets reports the paths it touches.
type Tool interface {
	Name() string
	Class() Class // what it does to the project, which is what decides how a call of it runs
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
	ExecTimeout    time.Duration
	CommandLogDir  string
	MaxResultBytes int
	Plan           *Plan // the steps of a long task, which the model writes and the screen shows
}

func BuiltIn(root *os.Root, dir string, opts Options) *Registry {
	read := NewReadFile(root, dir)
	write := NewWriteFile(root, dir)
	edit := NewEditFile(root, dir)
	for _, scope := range []*scope{&read.scope, &write.scope, &edit.scope} {
		scope.budget = opts.MaxResultBytes
	}
	read.Lines = opts.ReadLines

	run := NewExec(dir)
	run.Timeout = opts.ExecTimeout
	run.LogDir = opts.CommandLogDir
	run.MaxBytes = opts.MaxResultBytes

	plan := opts.Plan
	if plan == nil {
		plan = NewPlan()
	}

	return NewRegistry(read, write, edit, run, plan)
}

// Detail is what a tool can say about its last result beyond the text.
type Detail struct {
	Change string // for a change: all of it, for a screen; the model is sent the short diff in the result
}

// Detailed is a tool that keeps a little structure about the result it just returned. Only tools
// that never run beside each other may do this: writes and edits go one at a time, while the
// read-only tools can overlap and so keep nothing of the sort; the run asks no overlapping tool for
// one, whatever the tool keeps for itself.
type Detailed interface {
	LastDetail() Detail
}
