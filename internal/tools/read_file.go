package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"sync"
)

const (
	// defaultReadLimit is pi's window: a file up to the byte budget comes back in one call. Smaller
	// windows cost a model round-trip per page, and a 900-line file took a review seven of them.
	defaultReadLimit = 2000
	maxReadLimit     = 2000
)

// ReadFile returns a slice of a file, numbered like cat -n.
type ReadFile struct {
	scope
	Lines  int             // window when the model omits limit; 0 means defaultReadLimit
	mu     sync.Mutex      // read-only calls can overlap, and this map decides what they are told
	served map[string]bool // windows already handed over, so a repeat costs one line
}

func (r *ReadFile) window() int {
	if r.Lines > 0 {
		return r.Lines
	}
	return defaultReadLimit
}

type readInput struct {
	Path    string `json:"path"`
	Offset  int    `json:"offset"`
	Limit   int    `json:"limit"`
	Outline bool   `json:"outline"`
}

func NewReadFile(root *os.Root, dir string) *ReadFile {
	return &ReadFile{scope: scope{root: root, dir: dir}, served: map[string]bool{}}
}

func (*ReadFile) Name() string { return "read_file" }

func (*ReadFile) Description() string {
	return "Read a text file. The header gives the range and total lines; the text has no line numbers. outline=true lists declarations with their line ranges: use it on a big file first."
}

func (*ReadFile) NeedsConfirm() bool { return false }

func (*ReadFile) Schema() json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{
	  "type": "object",
	  "properties": {
	    "path": {"type": "string", "description": "file path inside the project, e.g. src/main.go"},
	    "offset": {"type": "integer", "description": "first line, 1-based"},
	    "limit": {"type": "integer", "description": "lines (default %d)"},
	    "outline": {"type": "boolean"}
	  },
	  "required": ["path"]
	}`, defaultReadLimit))
}

func (t *ReadFile) Targets(args json.RawMessage) ([]Target, error) {
	var in readInput
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	return []Target{resolveTarget(in.Path, t.dir)}, nil
}

func (t *ReadFile) Run(ctx context.Context, args json.RawMessage) (string, error) {
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
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("read_file %q: no such file; find it with find_files, or list the directory with depth 1", in.Path)
	}
	if err != nil {
		return "", toolError(t.Name(), in.Path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("read_file %q is a directory; list it with find_files pattern '*' and depth 1", in.Path)
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
	if in.Outline {
		outline := fmt.Sprintf("%s: %d lines\n%s", h.shown(), len(lines), outlineText(h.name, lines))
		if t.alreadyShown(outline) {
			return fmt.Sprintf("%s: you already have this outline of %d lines and the file has not changed; read a range or search_text for a line", h.shown(), len(lines)), nil
		}
		return outline, nil
	}
	first := in.Offset
	if first < 1 {
		first = 1
	}
	if first > len(lines) {
		return "", fmt.Errorf("read_file %q: offset %d is past the end of a %d line file", in.Path, first, len(lines))
	}
	window, unasked := t.window(), in.Limit == 0 && in.Offset <= 1
	last := first - 1 + clampLimit(in.Limit, window, maxReadLimit)
	if last > len(lines) {
		last = len(lines)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s: lines %d-%d of %d\n", h.shown(), first, last, len(lines))
	shown := first - 1
	for n := first; n <= last; n++ {
		entry := lines[n-1] + "\n"
		if b.Len()+len(entry) > t.aim() {
			// one huge line (a minified asset) still gets its head shown rather than nothing
			if n == first {
				b.WriteString(cutUTF8(lines[n-1], t.aim()-b.Len()-64) + "\n")
				shown = n
			}
			break
		}
		b.WriteString(entry)
		shown = n
	}
	if shown < len(lines) {
		reason := "pass a larger limit"
		if shown < last {
			reason = fmt.Sprintf("%d byte limit", t.aim())
		}
		fmt.Fprintf(&b, "\n[%d of %d lines shown (%s); continue with offset=%d]\n", shown-first+1, len(lines), reason, shown+1)
		if unasked {
			fmt.Fprintf(&b, "[a long file: to find a line, search_text with path %q is faster than reading on]\n", h.shown())
		}
	}
	text := b.String()
	if t.alreadyShown(text) {
		return fmt.Sprintf("%s: unchanged since you read lines %d-%d; that window is already in front of you, so read another range or search_text for a line",
			h.shown(), first, last), nil
	}
	return text, nil
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

// alreadyShown records one result and says whether that exact text went to the model before. The
// text is the key, so a different window, an edited file or a different cut is a different result.
func (t *ReadFile) alreadyShown(text string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.served == nil {
		t.served = map[string]bool{}
	}
	key := resultKey(text)
	if t.served[key] {
		return true
	}
	t.served[key] = true
	return false
}

// Forget drops one result from the memory: the conversation no longer holds that text, so reading
// the same window again has to bring the text back rather than a pointer to it.
func (t *ReadFile) Forget(shown Shown) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.served, resultKey(shown.Text))
}

// ForgetAll empties the memory, which is what a reset does to the conversation.
func (t *ReadFile) ForgetAll() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.served = map[string]bool{}
}
