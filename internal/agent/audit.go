package agent

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/audit"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// record appends one line to the audit log. A broken log never stops a run: it is reported once.
func (a *Agent) record(id, name, argsText string, targets []tools.Target, outside []string, decision, status, result string, detail tools.Detail, start time.Time) {
	if a.opts.Audit == nil {
		return
	}
	entry := audit.Entry{
		ID:          id,
		Turn:        a.turn,
		Root:        a.opts.Root,
		Model:       a.model,
		Tool:        name,
		Args:        argsText,
		Paths:       targetPaths(targets),
		OutsideRoot: outside,
		Decision:    decision,
		Status:      status,
		ExitCode:    detail.ExitCode,
		DurationMS:  time.Since(start).Milliseconds(),
		OutputPath:  detail.LogPath,
		Diff:        detail.Diff,
	}
	if result != "" {
		entry.ResultBytes = len(result)
		sum := sha256.Sum256([]byte(result))
		entry.ResultSHA = fmt.Sprintf("%x", sum[:8])
	}
	if err := a.opts.Audit.Append(entry); err != nil && !a.auditBroken.Swap(true) {
		// a log that will not write is reported once, not per call
		a.tell(err.Error())
	}
}

func describe(name, argsText string, outside []string) string {
	if len(outside) == 0 {
		return name + " " + argsText
	}
	return fmt.Sprintf("%s OUTSIDE the project root: %s\n  %s", name, strings.Join(outside, ", "), oneLine(argsText, 120))
}

// oneLine squashes s onto one short line for logging.
func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "..."
	}
	return s
}

// targetPaths lists what a call touches, for the log.
func targetPaths(targets []tools.Target) []string {
	paths := make([]string, 0, len(targets))
	for _, t := range targets {
		paths = append(paths, t.Resolved)
	}
	return paths
}

// detailOf asks a tool what its last result says beyond the text.
func detailOf(tool tools.Tool) tools.Detail {
	if d, ok := tool.(tools.Detailed); ok {
		return d.LastDetail()
	}
	return tools.Detail{}
}

// History reports what this run's audit log says about the calls it made.
// History answers /history: a tool name, "failed", "outside", an id like r12, or --all to
// widen the scope from this run to every run of this root.
func (a *Agent) History(query string, limit int) string {
	if a.opts.Audit == nil {
		return "this run has no audit log"
	}
	query, all := splitAll(query)
	entries, damaged, err := a.logged(all)
	if err != nil {
		return err.Error()
	}
	if len(entries) == 0 {
		return "nothing has been logged yet" + audit.Skipped(damaged)
	}
	return audit.Answer(entries, query, limit, damaged)
}

func (a *Agent) logged(all bool) ([]audit.Entry, int, error) {
	if all {
		return a.opts.Audit.ReadRoot(a.opts.Root)
	}
	return a.opts.Audit.Read(a.opts.Audit.Session())
}
