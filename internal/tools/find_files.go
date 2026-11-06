package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	defaultFindResults = 1000
	maxFindResults     = 5000
)

// FindFiles lists files by glob with fd when installed, or the Go walker, sorted either way.
type FindFiles struct {
	scope
	fdPath string
}

type findInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path"`
	Limit   int    `json:"limit"`
}

func NewFindFiles(root *os.Root, dir string) *FindFiles {
	fd, _ := exec.LookPath("fd")
	if fd == "" {
		fd, _ = exec.LookPath("fdfind") // Debian and Ubuntu ship it under this name
	}
	return &FindFiles{scope: scope{root: root, dir: dir}, fdPath: fd}
}

func (FindFiles) Name() string { return "find_files" }

func (FindFiles) Description() string {
	return "Find files by glob pattern: '*.go', '*_test.go', 'internal/**/*.go'. Returns sorted project-relative paths; .gitignore entries and .git are skipped. Default 1000 results."
}

func (FindFiles) NeedsConfirm() bool { return false }

func (FindFiles) Schema() json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{
	  "type": "object",
	  "properties": {
	    "pattern": {"type": "string", "description": "glob to match, e.g. *.go or internal/**/*.go"},
	    "path": {"type": "string", "description": "directory to search (default the project root)"},
	    "limit": {"type": "integer", "description": "max paths to return (default %d, max %d)"}
	  },
	  "required": ["pattern"]
	}`, defaultFindResults, maxFindResults))
}

func (t FindFiles) Targets(args json.RawMessage) ([]Target, error) {
	var in findInput
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	target, err := resolveTarget(in.Path, t.dir)
	return []Target{target}, err
}

func (t FindFiles) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in findInput
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	if strings.TrimSpace(in.Pattern) == "" {
		return "", errors.New("find_files: pattern is empty")
	}
	limit := clampLimit(in.Limit, defaultFindResults, maxFindResults)

	h, err := t.open(ctx, in.Path)
	if err != nil {
		return "", toolError(t.Name(), in.Path, err)
	}
	defer h.close()

	var found []string
	if t.fdPath != "" {
		found, err = t.withFd(ctx, h, in.Pattern, limit)
	} else {
		found, err = t.withGoWalker(ctx, h, in.Pattern, limit)
	}
	if err != nil {
		return "", err
	}
	slices.Sort(found)
	extra := len(found) > limit
	if extra {
		found = found[:limit]
	}
	if len(found) == 0 {
		return "(no files matched)", nil
	}
	b := strings.Builder{}
	for i, p := range found {
		if b.Len()+len(p)+1 > resultBudget {
			fmt.Fprintf(&b, "\n[... %d more paths; output hit the %d byte budget: narrow the pattern or lower limit]", len(found)-i, resultBudget)
			break
		}
		b.WriteString(p)
		b.WriteString("\n")
	}
	if extra {
		fmt.Fprintf(&b, "\n[stopped at the %d result limit; narrow the pattern or raise limit]", limit)
	}
	return b.String(), nil
}

// withFd runs fd in the searched dir with "**/"-prefixed globs matched against the whole path.
func (t FindFiles) withFd(ctx context.Context, h handle, pattern string, limit int) ([]string, error) {
	cmd := groupCommand(ctx, t.fdPath,
		"--glob", "--full-path", "**/"+pattern,
		"--hidden", "--no-require-git", "--exclude", skipDirName,
		"--max-results", strconv.Itoa(limit+1),
		".")
	cmd.Dir = h.target.Resolved
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("find_files: fd: %s", msg)
		}
		return nil, fmt.Errorf("find_files: fd: %w", err)
	}
	var found []string
	for line := range strings.SplitSeq(strings.TrimSpace(stdout.String()), "\n") {
		if line == "" {
			continue
		}
		found = append(found, displayPath(t.dir, absolute(h.target.Resolved, line)))
	}
	return found, nil
}

func (t FindFiles) withGoWalker(ctx context.Context, h handle, pattern string, limit int) ([]string, error) {
	re, err := compileGlob(pattern)
	if err != nil {
		return nil, fmt.Errorf("find_files: bad pattern %q: %v", pattern, err)
	}
	var found []string
	err = walkNames(ctx, h.root, h.name, func(path string, _ []byte) error {
		if re.MatchString(path) {
			found = append(found, h.display(path))
			if len(found) > limit {
				return stopWalk
			}
		}
		return nil
	})
	return found, err
}

// compileGlob anchors a glob at the end of a slash path but lets it start at any depth.
func compileGlob(pattern string) (*regexp.Regexp, error) {
	body := globRegexp(strings.TrimPrefix(pattern, "./"))
	if body == "" {
		return nil, errors.New("empty pattern")
	}
	if strings.Contains(pattern, "/") {
		body = `(^|/)` + body
	} else {
		body = `(^|.*/)` + body
	}
	return regexp.Compile(body + `$`)
}
