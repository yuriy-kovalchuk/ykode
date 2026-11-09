package tools

import (
	"context"
	"errors"
	"fmt"
	"regexp"
)

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
	// one named file is read whole, so it gets the limit the walker already applies to a tree
	if info.Size() > maxScannedFileBytes {
		return false, fmt.Errorf("search_text %q is %.1f MB, over the %d MB it reads; look inside it with exec_command, e.g. grep -n PATTERN %s",
			in.Path, float64(info.Size())/(1<<20), maxScannedFileBytes/(1<<20), in.Path)
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
