// Package tools holds the callable surface the agent exposes to the model: an answer is text, and
// reading a file can put a picture beside it.
package tools

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
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

// Result is what a call came back with: the text the model reads, and beside it the parts that are not
// text at all. Only reading a file can produce the second kind.
type Result struct {
	Text        string
	Attachments []llm.Attachment
}

// Attaching is a tool whose answer can carry an attachment. Run stays the text of that answer, which is
// all a screen, a summary or a result budget cares about; the run asks the other question when it builds
// the message the model reads. A tool answers a call this way without keeping anything between calls, so
// the read-only tools that run beside each other may still do it.
type Attaching interface {
	RunWithAttachments(ctx context.Context, args json.RawMessage) (Result, error)
}

// CallOf runs a tool with whatever its answer carries beside the text.
func CallOf(tool Tool, ctx context.Context, args json.RawMessage) (Result, error) {
	if with, ok := tool.(Attaching); ok {
		return with.RunWithAttachments(ctx, args)
	}
	text, err := tool.Run(ctx, args)
	return Result{Text: text}, err
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
