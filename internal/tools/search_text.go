package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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

// noMatches is what a search that found nothing answers.
const noMatches = "(no matches)"

// elsewhere answers a filtered search that found nothing: a guessed glob is the usual reason a
// model never sees the file it is after, so say where the pattern does occur.
func (t SearchText) elsewhere(ctx context.Context, h handle, in searchInput) string {
	glob := in.Glob
	in.Glob, in.Mode = "", modeFiles
	other, err := t.summary(ctx, h, in, 5)
	if err != nil || other == noMatches {
		return noMatches
	}
	return fmt.Sprintf("(no matches in files matching %q, but the pattern occurs in:)\n%s\n[search again without glob, or with one that covers these]",
		glob, strings.TrimSpace(other))
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

// withGoWalker is the fallback when ripgrep is not installed.
func (t SearchText) withGoWalker(ctx context.Context, h handle, in searchInput, limit int) ([]hit, int, error) {
	re, globRe, err := t.matcher(in)
	if err != nil {
		return nil, 0, err
	}

	var hits []hit
	scanned := 0
	matches := 0
	stop := false
	scanFile := func(path string, data []byte) error {
		if !wanted(globRe, path) {
			return nil
		}
		scanned++
		lines := splitLines(data)
		keep := map[int]bool{}
		for n, line := range lines {
			if !re.MatchString(line) {
				continue
			}
			matches++
			for c := n - in.Context; c <= n+in.Context; c++ {
				if c >= 0 && c < len(lines) {
					keep[c] = true
				}
			}
			if matches >= limit {
				stop = true
				break
			}
		}
		for c := range lines {
			if keep[c] {
				hits = append(hits, hit{
					path:    h.display(path),
					line:    c + 1,
					text:    lines[c],
					context: !re.MatchString(lines[c]),
				})
			}
		}
		if stop {
			return stopWalk
		}
		return nil
	}
	if done, err := t.scanOne(h, in, scanFile); done || err != nil {
		return hits, scanned, err
	}
	err = walkFiles(ctx, h.root, h.name, scanFile)
	return hits, scanned, err
}

// matcher compiles the search pattern and the optional glob filter.
func (t SearchText) matcher(in searchInput) (*regexp.Regexp, *glob, error) {
	pattern := in.Pattern
	if in.Literal {
		pattern = regexp.QuoteMeta(pattern)
	}
	if in.IgnoreCase {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, nil, fmt.Errorf("search_text: bad pattern %q: %v", in.Pattern, err)
	}
	var nameMatch *glob
	if in.Glob != "" {
		nameMatch, err = newGlob(in.Glob)
		if err != nil {
			return nil, nil, fmt.Errorf("search_text: bad glob %q: %v", in.Glob, err)
		}
	}
	return re, nameMatch, nil
}

// wanted applies the optional glob filter to a walked path.
func wanted(nameMatch *glob, path string) bool {
	return nameMatch == nil || nameMatch.match(path)
}

// scanOne searches the single file the path names, reporting false when it is a directory.
func (t SearchText) scanOne(h handle, in searchInput, scan func(path string, data []byte) error) (bool, error) {
	info, err := h.root.Stat(h.name)
	if err != nil || info.IsDir() {
		return false, nil
	}
	data, readErr := h.root.ReadFile(h.name)
	if readErr != nil {
		return false, toolError(t.Name(), in.Path, readErr)
	}
	if err := scan(h.name, data); err != nil && !errors.Is(err, stopWalk) {
		return false, err
	}
	return true, nil
}

// withGoSummary counts matches per file without keeping the lines.
func (t SearchText) withGoSummary(ctx context.Context, h handle, in searchInput, limit int) ([]fileMatch, int, bool, error) {
	re, globRe, err := t.matcher(in)
	if err != nil {
		return nil, 0, false, err
	}
	var files []fileMatch
	scanned := 0
	countFile := func(path string, data []byte) error {
		if !wanted(globRe, path) {
			return nil
		}
		scanned++
		matches := 0
		for _, line := range splitLines(data) {
			if !re.MatchString(line) {
				continue
			}
			matches++
			if in.Mode == modeFiles {
				break
			}
		}
		if matches == 0 {
			return nil
		}
		files = append(files, fileMatch{path: h.display(path), matches: matches})
		if len(files) >= limit {
			return stopWalk
		}
		return nil
	}
	if done, err := t.scanOne(h, in, countFile); done || err != nil {
		return files, scanned, len(files) >= limit, err
	}
	if walkErr := walkFiles(ctx, h.root, h.name, countFile); walkErr != nil {
		return nil, scanned, false, walkErr
	}
	return files, scanned, len(files) >= limit, nil
}

// formatSummary renders one line per file plus the total the model asked for.
func formatSummary(mode string, files []fileMatch, limit, scanned int, stopped bool, budget int) string {
	if len(files) == 0 {
		return noMatches
	}
	var b strings.Builder
	total := 0
	for i, f := range files {
		line := f.path
		if mode != modeFiles { // files_with_matches never counted, so it must not pretend to
			line += ":" + strconv.Itoa(f.matches)
		}
		if b.Len()+len(line)+1 > budget {
			return b.String() + fmt.Sprintf(
				"\n\n[... %d more files; output hit the %d byte budget: narrow the pattern or pass glob]", len(files)-i, budget)
		}
		total += f.matches
		b.WriteString(line)
		b.WriteString("\n")
	}
	if stopped {
		note := fmt.Sprintf("[stopped at the %d file limit; narrow the pattern, pass glob, or raise limit]", limit)
		if scanned > 0 { // ripgrep reports no file count, so the note would only guess
			note = fmt.Sprintf("[stopped at the %d file limit after %d files; narrow the pattern, pass glob, or raise limit]", limit, scanned)
		}
		b.WriteString("\n" + note)
		return b.String()
	}
	if mode == modeFiles {
		fmt.Fprintf(&b, "%d files matched\n", len(files))
	} else {
		fmt.Fprintf(&b, "%d matches in %d files\n", total, len(files))
	}
	return b.String()
}

// formatHits renders hits and keeps the result inside the tool budget. A file with several
// matches gets one header, which is the path once instead of on every line.
func formatHits(hits []hit, limit, scanned, budget int) string {
	if len(hits) == 0 {
		return noMatches
	}
	perFile := map[string]int{}
	for _, h := range hits {
		perFile[h.path]++
	}
	// one header per file, whether the lines around it are matches or context: the path is what
	// repeats, and it is the same path every time
	grouped := func(path string) bool { return perFile[path] > 1 }

	var b strings.Builder
	for i, h := range hits {
		sep := ":"
		if h.context {
			sep = "-"
		}
		body := strconv.Itoa(h.line) + sep + oneLine(h.text, searchLineChars)
		line := h.path + sep + body
		if grouped(h.path) {
			line = "  " + body
			if i == 0 || hits[i-1].path != h.path {
				line = h.path + ":\n  " + body
			}
		}
		if b.Len()+len(line)+1 > budget {
			return b.String() + fmt.Sprintf(
				"\n\n[... %d more lines; output hit the %d byte budget: narrow the pattern, pass glob, or lower limit]", len(hits)-i, budget)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	if len(hits) >= limit {
		note := fmt.Sprintf("[stopped at the %d match limit; narrow the pattern, pass glob, or raise limit]", limit)
		if scanned > 0 {
			note = fmt.Sprintf("[stopped at the %d match limit after %d files; narrow the pattern, pass glob, or raise limit]", limit, scanned)
		}
		b.WriteString("\n" + note)
	}
	return b.String()
}

// oneLine trims the end of a line and cuts it when long, keeping output scannable.
func oneLine(s string, maxChars int) string {
	s = strings.TrimRight(s, " \t\r") // leading tabs stay: a line copied from here becomes edit_file's old_text
	if len(s) > maxChars {
		return cutUTF8(s, maxChars) + " […cut]"
	}
	return s
}

// displayPath keeps paths project-relative because that is what read_file wants next.
func displayPath(projectRoot, abs string) string {
	rel, err := filepath.Rel(projectRoot, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return abs
	}
	return filepath.ToSlash(rel)
}
