package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// ListDir lists one directory inside the project root.
type ListDir struct{ scope }

const maxListEntries = 200

type listInput struct {
	Path string `json:"path"`
}

func NewListDir(root *os.Root, dir string) *ListDir {
	return &ListDir{scope{root: root, dir: dir}}
}

func (ListDir) Name() string { return "list_dir" }

func (ListDir) Description() string {
	return "List the entries of a directory inside the project root. Directory names end with '/'. Paths outside the root are allowed but need approval."
}

func (ListDir) NeedsConfirm() bool { return false }

func (ListDir) Schema() json.RawMessage {
	return json.RawMessage(`{
	  "type": "object",
	  "properties": {
	    "path": {"type": "string", "description": "directory relative to the project root; \".\" or empty means the root. Absolute paths and paths outside the root are allowed but need approval"}
	  }
	}`)
}

func (t ListDir) Targets(args json.RawMessage) ([]Target, error) {
	var in listInput
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	target, err := resolveTarget(in.Path, t.dir)
	return []Target{target}, err
}

func (t ListDir) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var in listInput
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	h, err := t.open(ctx, in.Path)
	if err != nil {
		return "", toolError(t.Name(), in.Path, err)
	}
	defer h.close()
	dir, err := h.root.Open(h.name)
	if err != nil {
		return "", toolError(t.Name(), in.Path, err)
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return "", toolError(t.Name(), in.Path, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	if len(entries) == 0 {
		return "(empty directory)", nil
	}

	var b strings.Builder
	for i, e := range entries {
		// A cut here must say how to continue; the agent's generic byte cut cannot.
		if i == maxListEntries || b.Len() > resultBudget {
			fmt.Fprintf(&b, "[... %d more entries of %d; list a subdirectory or use find_files]\n",
				len(entries)-i, len(entries))
			break
		}
		b.WriteString(e.Name())
		if e.IsDir() {
			b.WriteString("/")
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}
