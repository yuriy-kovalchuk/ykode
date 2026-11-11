package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
)

// WriteFile creates or replaces a whole file, making parent directories.
type WriteFile struct {
	scope
	last Detail // the diff of the write that just ran, for the audit log
}

type writeInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func NewWriteFile(root *os.Root, dir string) *WriteFile {
	return &WriteFile{scope: scope{root: root, dir: dir}}
}

func (*WriteFile) Name() string { return "write_file" }

func (*WriteFile) Description() string {
	return "Create a file or replace it whole; parent directories are created. Use edit_file to change part of a file."
}

func (*WriteFile) NeedsConfirm() bool { return true }

func (*WriteFile) Schema() json.RawMessage {
	return json.RawMessage(`{
	  "type": "object",
	  "properties": {
	    "path": {"type": "string", "description": "file path inside the project, e.g. src/main.go"},
	    "content": {"type": "string", "description": "complete file contents"}
	  },
	  "required": ["path", "content"]
	}`)
}

func (t *WriteFile) Targets(args json.RawMessage) ([]Target, error) {
	var in writeInput
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	return []Target{resolveTarget(in.Path, t.dir)}, nil
}

func (t *WriteFile) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// LastDetail describes this call, so an old diff must not be logged against a new one
	t.last = Detail{}
	var in writeInput
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	h, err := t.open(ctx, in.Path)
	if err != nil {
		return "", toolError(t.Name(), in.Path, err)
	}
	defer h.close()
	if h.name == "." || h.name == "" {
		return "", fmt.Errorf("write_file: path must name a file, not the root")
	}
	if dir := path.Dir(h.name); dir != "." {
		if err := h.root.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("write_file %q: mkdir %q: %w", in.Path, dir, err)
		}
	}
	before, err := h.root.ReadFile(h.name)
	existed := err == nil
	if err := h.root.WriteFile(h.name, []byte(in.Content), 0o644); err != nil {
		return "", toolError(t.Name(), in.Path, err)
	}
	t.pushUndo(h, before, []byte(in.Content), existed)      // only a change that happened is undone
	t.last.Change = wholeChange(string(before), in.Content) // a new file is a change from nothing
	if !existed {
		return fmt.Sprintf("created %s (%d bytes, %d lines)",
			h.shown(), len(in.Content), len(diffSplit(in.Content))), nil
	}
	result := fmt.Sprintf("wrote %s (%d bytes)", h.shown(), len(in.Content))
	if diff := diffText(string(before), in.Content); diff != "" {
		t.last.Diff = diff
		result += "\n" + diff
	}
	return result, nil
}

// LastDetail returns the diff of the write that just ran.
func (t *WriteFile) LastDetail() Detail { return t.last }
