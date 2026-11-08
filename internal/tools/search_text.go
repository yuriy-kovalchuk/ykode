package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

const (
	defaultSearchMatches = 100
	maxSearchMatches     = 500
	maxSearchContext     = 5
	searchLineChars      = 500
	searchTimeout        = 30 * time.Second
)

// SearchText greps with rg when installed, else the Go walker; the output is identical either way.
type SearchText struct {
	scope
	rgPath     string
	MatchLimit int // match cap when the model does not ask for one; 0 means defaultSearchMatches
}

// search modes: content answers with lines, the summaries answer with files.
const (
	modeContent = "content"
	modeCount   = "count"
	modeFiles   = "files_with_matches"
)

type searchInput struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path"`
	Glob       string `json:"glob"`
	Mode       string `json:"mode"`
	IgnoreCase bool   `json:"ignore_case"`
	Literal    bool   `json:"literal"`
	Context    int    `json:"context"`
	Limit      int    `json:"limit"`
}

// MatchLimit is the match cap when the model does not ask for one; 0 means defaultSearchMatches.
func (s SearchText) matches() int {
	if s.MatchLimit > 0 {
		return s.MatchLimit
	}
	return defaultSearchMatches
}

func NewSearchText(root *os.Root, dir string) *SearchText {
	rg, _ := exec.LookPath("rg")
	return &SearchText{scope: scope{root: root, dir: dir}, rgPath: rg}
}

func (SearchText) Name() string { return "search_text" }

func (SearchText) Description() string {
	return "Search file contents by regex, like rg: path:line:text. To ask where rather than what, mode=files_with_matches (paths) or count (per file) costs far less."
}

func (SearchText) NeedsConfirm() bool { return false }

func (SearchText) Schema() json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{
	  "type": "object",
	  "properties": {
	    "pattern": {"type": "string", "description": "regex, or plain text with literal=true"},
	    "path": {"type": "string", "description": "file or directory"},
	    "glob": {"type": "string", "description": "only files matching, e.g. *.go"},
	    "mode": {"type": "string", "enum": ["content", "count", "files_with_matches"]},
	    "ignore_case": {"type": "boolean"},
	    "literal": {"type": "boolean"},
	    "context": {"type": "integer", "description": "lines around each match (max %d)"},
	    "limit": {"type": "integer", "description": "max results (default %d)"}
	  },
	  "required": ["pattern"]
	}`, maxSearchContext, defaultSearchMatches))
}

func (t SearchText) Targets(args json.RawMessage) ([]Target, error) {
	var in searchInput
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	return []Target{resolveTarget(in.Path, t.dir)}, nil
}

func (t SearchText) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in searchInput
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	if in.Pattern == "" {
		return "", errors.New("search_text: pattern is empty")
	}
	if in.Mode == "" {
		in.Mode = modeContent
	}
	if in.Mode != modeContent && in.Mode != modeCount && in.Mode != modeFiles {
		return "", fmt.Errorf("search_text: unknown mode %q; use content, count or files_with_matches", in.Mode)
	}
	if in.Mode != modeContent {
		in.Context = 0
	}
	in.Context = clamp(in.Context, 0, maxSearchContext)
	limit := clampLimit(in.Limit, t.matches(), maxSearchMatches)

	h, err := t.open(ctx, in.Path)
	if err != nil {
		return "", toolError(t.Name(), in.Path, err)
	}
	defer h.close()

	out, err := t.search(ctx, h, in, limit)
	if err != nil || in.Glob == "" || out != noMatches {
		return out, err
	}
	return t.elsewhere(ctx, h, in), nil
}

// search runs one search with the filters as given.
func (t SearchText) search(ctx context.Context, h handle, in searchInput, limit int) (string, error) {
	if in.Mode != modeContent {
		return t.summary(ctx, h, in, limit)
	}
	if t.rgPath != "" {
		hits, err := t.withRipgrep(ctx, h, in, limit)
		if err != nil {
			return "", err
		}
		return formatHits(hits, limit, 0, t.aim()), nil
	}
	hits, scanned, err := t.withGoWalker(ctx, h, in, limit)
	if err != nil {
		return "", err
	}
	return formatHits(hits, limit, scanned, t.aim()), nil
}

// summary answers count and files_with_matches with one line per file instead of one per match.
func (t SearchText) summary(ctx context.Context, h handle, in searchInput, limit int) (string, error) {
	var files []fileMatch
	scanned, stopped := 0, false
	var err error
	if t.rgPath != "" {
		files, scanned, stopped, err = t.withRipgrepSummary(ctx, h, in, limit)
	} else {
		files, scanned, stopped, err = t.withGoSummary(ctx, h, in, limit)
	}
	if err != nil {
		return "", err
	}
	return formatSummary(in.Mode, files, limit, scanned, stopped, t.aim()), nil
}

// fileMatch is one file's tally in the summary modes.
type fileMatch struct {
	path    string
	matches int
}
