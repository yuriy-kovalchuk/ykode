package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

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
