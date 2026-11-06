package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const (
	defaultReadLimit = 400 // lines per read_file call when the model omits limit
	maxReadLimit     = 2000
)

// ReadFile returns a slice of a file, numbered like cat -n.
type ReadFile struct{ scope }

type readInput struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

func NewReadFile(root *os.Root, dir string) *ReadFile {
	return &ReadFile{scope{root: root, dir: dir}}
}

func (ReadFile) Name() string { return "read_file" }

func (ReadFile) Description() string {
	return "Read a text file as numbered lines, which are the exact text edit_file's old_text must match. Page long files with offset and limit; the result says where to continue."
}

func (ReadFile) NeedsConfirm() bool { return false }

func (ReadFile) Schema() json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{
	  "type": "object",
	  "properties": {
	    "path": {"type": "string", "description": "file path relative to the project root (absolute paths allowed, but need approval)"},
	    "offset": {"type": "integer", "description": "1-based line to start at (default 1)"},
	    "limit": {"type": "integer", "description": "max lines to return (default %d, max %d; the byte budget can cut earlier)"}
	  },
	  "required": ["path"]
	}`, defaultReadLimit, maxReadLimit))
}

func (t ReadFile) Targets(args json.RawMessage) ([]Target, error) {
	var in readInput
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	target, err := resolveTarget(in.Path, t.dir)
	return []Target{target}, err
}

func (t ReadFile) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var in readInput
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	h, err := t.open(ctx, in.Path)
	if err != nil {
		return "", toolError(t.Name(), in.Path, err)
	}
	defer h.close()
	info, err := h.root.Stat(h.name)
	if err != nil {
		return "", toolError(t.Name(), in.Path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("read_file %q is a directory; use list_dir to see what is in it", in.Path)
	}
	// paging a file far bigger than the output budget would burn context one window at a time
	if info.Size() > maxScannedFileBytes {
		return "", fmt.Errorf("read_file %q is %.1f MB; use search_text to find a spot, or exec_command with sed -n '1,200p'",
			in.Path, float64(info.Size())/(1<<20))
	}
	data, err := h.root.ReadFile(h.name)
	if err != nil {
		return "", toolError(t.Name(), in.Path, err)
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return "", fmt.Errorf("read_file %q: looks like a binary file", in.Path)
	}

	lines := splitLines(data)
	if len(lines) == 0 {
		return "(empty file)", nil
	}
	first := in.Offset
	if first < 1 {
		first = 1
	}
	if first > len(lines) {
		return "", fmt.Errorf("read_file %q: offset %d is past the end of a %d line file", in.Path, first, len(lines))
	}
	last := first - 1 + clampLimit(in.Limit, defaultReadLimit, maxReadLimit)
	if last > len(lines) {
		last = len(lines)
	}

	var b strings.Builder
	shown := first - 1
	for n := first; n <= last; n++ {
		entry := fmt.Sprintf("%4d\t%s\n", n, lines[n-1])
		if b.Len()+len(entry) > resultBudget {
			// one huge line (a minified asset) still gets its head shown rather than nothing
			if n == first {
				b.WriteString(fmt.Sprintf("%4d\t%s\n", n, cutUTF8(lines[n-1], resultBudget-64)))
				shown = n
			}
			break
		}
		b.WriteString(entry)
		shown = n
	}
	if shown < len(lines) {
		reason := "line limit"
		if shown < last {
			reason = fmt.Sprintf("%d byte limit", resultBudget)
		}
		fmt.Fprintf(&b, "\n[showing lines %d-%d of %d (%s); continue with offset=%d]\n", first, shown, len(lines), reason, shown+1)
	}
	return b.String(), nil
}

// splitLines drops the empty element a trailing newline gives and CRLF's \r, matching edit_file's text.
func splitLines(data []byte) []string {
	s := strings.TrimSuffix(string(data), "\n")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "\n")
	for i, part := range parts {
		parts[i] = strings.TrimSuffix(part, "\r")
	}
	return parts
}
