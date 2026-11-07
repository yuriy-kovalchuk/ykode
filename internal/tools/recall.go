package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// Recall hands back tool output that was shortened out of the conversation.
type Recall struct {
	scope
	store  ResultStore
	mu     sync.Mutex      // read-only calls can overlap, and this map decides what they are told
	served map[string]bool // ids already handed over, so a second one costs a line
}

type recallInput struct {
	ID      string `json:"id"`
	Pattern string `json:"pattern"` // only the lines matching this regex, with a little context
	Offset  int    `json:"offset"`  // skip this many lines of the answer
	Limit   int    `json:"limit"`   // how many lines to return
}

const (
	recallContextLines = 2   // lines of context around a pattern match
	recallLineLimit    = 400 // a call that asks for no limit still has to end somewhere
)

func NewRecall(store ResultStore) *Recall {
	return &Recall{store: store, served: map[string]bool{}}
}

func (*Recall) Name() string { return "recall" }

func (*Recall) Description() string {
	return "Hand back an earlier tool result that was shortened to fit the context: whole, or with pattern only the matching lines and a little around them. The id comes from the summary, e.g. r7."
}

func (*Recall) NeedsConfirm() bool { return false }

func (*Recall) Schema() json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{
	  "type": "object",
	  "properties": {
	    "id": {"type": "string", "description": "result id from the summary, e.g. r7"},
	    "pattern": {"type": "string", "description": "regex: only the matching lines and %d of context"},
	    "offset": {"type": "integer", "description": "skip this many lines"},
	    "limit": {"type": "integer", "description": "at most this many lines (default %d)"}
	  },
	  "required": ["id"]
	}`, recallContextLines, recallLineLimit))
}

func (t *Recall) Targets(json.RawMessage) ([]Target, error) { return nil, nil }

// matchingLines keeps the lines a pattern hits plus a little of what surrounds them.
func matchingLines(lines []string, re *regexp.Regexp, context int) []string {
	keep := make([]bool, len(lines))
	for i, line := range lines {
		if !re.MatchString(line) {
			continue
		}
		for j := max(0, i-context); j <= min(len(lines)-1, i+context); j++ {
			keep[j] = true
		}
	}
	var out []string
	for i, want := range keep {
		if want {
			out = append(out, lines[i])
		}
	}
	return out
}

func (t *Recall) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in recallInput
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	if !t.store.Saved() {
		return "", fmt.Errorf("recall: this run keeps no results, so nothing can be handed back")
	}
	text, err := t.store.Load(in.ID)
	if err != nil {
		return "", err
	}
	if in.Pattern != "" || in.Offset > 0 {
		return t.part(in.ID, text, in)
	}

	t.mu.Lock()
	if t.served == nil {
		t.served = map[string]bool{}
	}
	already := t.served[in.ID]
	t.served[in.ID] = true
	t.mu.Unlock()
	if already {
		return fmt.Sprintf("%s was already handed over whole; it has not changed since. ask again with pattern or offset for one part of it, or work from what you have", in.ID), nil
	}

	header := fmt.Sprintf("saved result %s: %d bytes\n", in.ID, len(text))
	return header + FormatResult(text, t.aim()-len(header)), nil
}

// part answers with a slice of a stored result, which is what keeps a recall cheap.
func (t *Recall) part(id, text string, in recallInput) (string, error) {
	// a file ends with a newline; that is not an extra empty line to count
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	kept := lines
	if in.Pattern != "" {
		re, err := regexp.Compile(in.Pattern)
		if err != nil {
			return "", fmt.Errorf("recall: pattern %q is not a valid regex: %w", in.Pattern, err)
		}
		kept = matchingLines(lines, re, recallContextLines)
		if len(kept) == 0 {
			return "", fmt.Errorf("recall: %s has no lines matching %q (the pattern is a regex, case-sensitive)", id, in.Pattern)
		}
	}
	limit := recallLineLimit
	if in.Limit > 0 {
		limit = clamp(in.Limit, 1, recallLineLimit)
	}
	if in.Offset < 0 {
		in.Offset = 0
	}
	if in.Offset >= len(kept) {
		return fmt.Sprintf("%s has %d such lines; offset %d is past the end", id, len(kept), in.Offset), nil
	}
	window := kept[in.Offset:]
	more := 0
	if len(window) > limit {
		more = len(window) - limit
		window = window[:limit]
	}
	header := fmt.Sprintf("saved result %s: %d of %d lines, from offset %d\n", id, len(window), len(kept), in.Offset)
	body := header + strings.Join(window, "\n")
	if more > 0 {
		body += fmt.Sprintf("\n[... %d more lines: ask again with offset %d, or widen pattern]", more, in.Offset+len(window))
	}
	return FormatResult(body, t.aim()), nil
}
