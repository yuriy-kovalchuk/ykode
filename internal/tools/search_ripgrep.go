package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// rgFlags are the options every rg call shares.
func rgFlags(in searchInput) []string {
	args := []string{"--no-config", "--color=never", "--hidden"}
	if in.IgnoreCase {
		args = append(args, "--ignore-case")
	}
	if in.Literal {
		args = append(args, "--fixed-strings")
	}
	if in.Glob != "" {
		args = append(args, "--glob", in.Glob)
	}
	// --hidden brings .git back into view, so put it out again.
	args = append(args, "--glob", "!"+skipDirName)
	// --no-require-git makes rg honour .gitignore outside a git repository, like the fallback
	return append(args, "--no-require-git")
}

// withRipgrepSummary asks rg for per-file counts or names, a far smaller answer than the matches.
func (t SearchText) withRipgrepSummary(ctx context.Context, h handle, in searchInput, limit int) ([]fileMatch, int, bool, error) {
	runCtx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	flag := "--count"
	if in.Mode == modeFiles {
		flag = "--files-with-matches"
	}
	args := append(rgFlags(in), flag, "--", in.Pattern, h.target.Resolved)

	cmd := groupCommand(runCtx, t.rgPath, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, 0, false, fmt.Errorf("search_text: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, 0, false, fmt.Errorf("search_text: %w", err)
	}

	var files []fileMatch
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		text := strings.TrimSpace(sc.Text())
		matches := 1
		if in.Mode == modeCount {
			cut := strings.LastIndex(text, ":")
			if cut <= 0 {
				continue
			}
			count, err := strconv.Atoi(text[cut+1:])
			if err != nil {
				continue
			}
			matches, text = count, text[:cut]
		}
		files = append(files, fileMatch{path: displayPath(t.dir, text), matches: matches})
		if len(files) >= limit {
			cancel() // stop rg now, otherwise Wait blocks on the unread stdout pipe
			break
		}
	}
	if ctx.Err() != nil {
		return nil, 0, false, ctx.Err()
	}
	stopped := len(files) >= limit
	if err := cmd.Wait(); err != nil && !stopped {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() > 1 {
			return nil, 0, false, fmt.Errorf("search_text: rg: %s", strings.TrimSpace(stderr.String()))
		}
	}
	return files, 0, stopped, nil
}

// hit is one output line: a matching line or a line of context around one.
type hit struct {
	path    string
	line    int
	text    string
	context bool
}

// withRipgrep runs rg --json and turns its events into hits.
func (t SearchText) withRipgrep(ctx context.Context, h handle, in searchInput, limit int) ([]hit, error) {
	runCtx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	args := append(rgFlags(in), "--json", "--line-number")
	if in.Context > 0 {
		args = append(args, "--context", strconv.Itoa(in.Context))
	}
	args = append(args, "--", in.Pattern, h.target.Resolved)

	cmd := groupCommand(runCtx, t.rgPath, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("search_text: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("search_text: %w", err)
	}

	var hits []hit
	matches := 0
	stoppedEarly := false
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		h, ok := parseRgHit(sc.Bytes(), t.dir)
		if !ok {
			continue
		}
		hits = append(hits, h)
		if h.context {
			continue
		}
		if matches++; matches >= limit {
			stoppedEarly = true
			cancel() // stop rg now, otherwise Wait blocks on the unread stdout pipe
			break
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err := cmd.Wait(); err != nil && !stoppedEarly {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() > 1 {
			return nil, fmt.Errorf("search_text: rg: %s", strings.TrimSpace(stderr.String()))
		}
	}
	return hits, nil
}

type rgEvent struct {
	Kind string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber int `json:"line_number"`
	} `json:"data"`
}

// parseRgHit keeps only the match and context events out of rg's json stream.
func parseRgHit(line []byte, projectRoot string) (hit, bool) {
	var ev rgEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		return hit{}, false
	}
	if ev.Kind != "match" && ev.Kind != "context" {
		return hit{}, false
	}
	return hit{
		path:    displayPath(projectRoot, ev.Data.Path.Text),
		line:    ev.Data.LineNumber,
		text:    strings.TrimRight(ev.Data.Lines.Text, "\r\n"),
		context: ev.Kind == "context",
	}, true
}
