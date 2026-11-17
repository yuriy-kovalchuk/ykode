// Package tools holds the callable surface the agent exposes to the model: text, and pictures beside it
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

// Result is what a call came back with: the text the model reads, and the parts that are not text
type Result struct {
	Text        string
	Attachments []llm.Attachment
}

// Attaching is a tool whose answer can carry an attachment; Run stays the text of that answer
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

// BuiltIn is the shipped tool set in locate, read, change, run order, with the tools' settings
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

// Detailed keeps a little structure about the last result, so only non-overlapping tools may do it
type Detailed interface {
	LastDetail() Detail
}
