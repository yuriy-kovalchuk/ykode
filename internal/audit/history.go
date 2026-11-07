package audit

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// idRe names one logged call: /history r12 asks for a single call in full.
var idRe = regexp.MustCompile(`^r[0-9]{1,6}$`)

// Match keeps the calls a history question is about: an id selects that one call, anything else
// is a filter over the tools. "started" lines never make the cut - the final line has the outcome.
func Match(entries []Entry, what string, limit int) []Entry {
	what = strings.TrimSpace(what)
	var kept []Entry
	if idRe.MatchString(what) {
		for _, e := range entries {
			if e.ID == what {
				kept = append(kept, e)
			}
		}
		return kept
	}
	for _, e := range entries {
		if e.Status != "started" && wanted(e, what) {
			kept = append(kept, e)
		}
	}
	if limit > 0 && len(kept) > limit {
		kept = kept[len(kept)-limit:]
	}
	return kept
}

// Answer replies to a history question in text: an id gets that call in full, the rest a table.
func Answer(entries []Entry, what string, limit int) string {
	kept := Match(entries, what, limit)
	q := strings.TrimSpace(what)
	if len(kept) == 0 {
		if idRe.MatchString(q) {
			return "no call " + q + " in the log"
		}
		return "nothing matches " + q
	}
	if idRe.MatchString(q) {
		return kept[len(kept)-1].Detail()
	}
	return table(kept)
}

// Format renders entries as a table, newest last. filter is a tool name, "failed" or "outside".
func Format(entries []Entry, filter string, limit int) string {
	return table(Match(entries, filter, limit))
}

func table(kept []Entry) string {
	var b strings.Builder
	for _, e := range kept {
		fmt.Fprintf(&b, "%-4s %s %-13s %-8s %s", e.ID, e.Time.Format("15:04:05"), e.Tool, e.Status, strings.Join(shortNames(e.Paths), " "))
		if e.ExitCode != nil {
			fmt.Fprintf(&b, " exit=%d", *e.ExitCode)
		}
		if len(e.OutsideRoot) > 0 {
			fmt.Fprintf(&b, " OUTSIDE")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// wanted accepts a tool name, "failed" or "outside"; the leading -- of the command line is ignored.
func wanted(e Entry, filter string) bool {
	switch strings.TrimPrefix(filter, "--") {
	case "":
		return true
	case "failed":
		return e.Status == "error" || e.Status == "refused" || e.Status == "timeout"
	case "outside":
		return len(e.OutsideRoot) > 0
	default:
		return strings.Contains(e.Tool, filter)
	}
}

// Detail renders one call in full, the answer to /history r12.
func (e Entry) Detail() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %s  %s\n", e.ID, e.Time.Format("2006-01-02 15:04:05"), e.Tool)
	fmt.Fprintf(&b, "  session %s · turn %d · %s\n", e.Session, e.Turn, e.Model)
	fmt.Fprintf(&b, "  decision %s · status %s", e.Decision, e.Status)
	if e.ExitCode != nil {
		fmt.Fprintf(&b, " · exit %d", *e.ExitCode)
	}
	fmt.Fprintf(&b, " · %dms\n", e.DurationMS)
	fmt.Fprintf(&b, "  args %s\n", e.Args)
	if len(e.Paths) > 0 {
		fmt.Fprintf(&b, "  paths %s\n", strings.Join(e.Paths, " "))
	}
	if len(e.OutsideRoot) > 0 {
		fmt.Fprintf(&b, "  outside the root: %s\n", strings.Join(e.OutsideRoot, " "))
	}
	if e.Diff != "" {
		fmt.Fprintf(&b, "  diff:\n%s\n", indent(e.Diff))
	}
	if e.OutputPath != "" {
		fmt.Fprintf(&b, "  full output (%d bytes): %s\n", e.ResultBytes, e.OutputPath)
	}
	return b.String()
}

func indent(text string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		lines[i] = "    " + line
	}
	return strings.Join(lines, "\n")
}

func shortNames(paths []string) []string {
	short := make([]string, len(paths))
	for i, p := range paths {
		if name := filepath.Base(p); name != "/" && name != "." {
			short[i] = name
		} else {
			short[i] = p
		}
	}
	return short
}
