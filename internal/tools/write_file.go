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
	return "Write a file inside the project root, replacing it if it exists. Parent directories are created. Prefer edit_file for small changes to an existing file. Paths outside the root are allowed but need approval."
}

func (*WriteFile) NeedsConfirm() bool { return true }

func (*WriteFile) Schema() json.RawMessage {
	return json.RawMessage(`{
	  "type": "object",
	  "properties": {
	    "path": {"type": "string", "description": "file path relative to the project root (absolute paths allowed, but need approval)"},
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
	target, err := resolveTarget(in.Path, t.dir)
	return []Target{target}, err
}

func (t *WriteFile) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
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
