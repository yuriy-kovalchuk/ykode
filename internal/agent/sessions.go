package agent

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/session"
)

const (
	sessionsLimit   = 20             // conversations one /sessions shows
	firstAskShown   = 60             // of an ask, before the listing says there is more
	sessionsWhenFmt = "02 Jan 15:04" // when a conversation was last written to
)

// Sessions lists the conversations that can be resumed, newest first, marking this project's and
// this run's. An id is a timestamp to the millisecond, so what each was about is what identifies it.
func (a *Agent) Sessions(limit int) string {
	path := a.opts.Session.Path()
	if path == "" {
		return "this run saves no conversation, so there is nothing to resume"
	}
	list, err := session.Describe(filepath.Dir(path), limit)
	if err != nil {
		return "the saved conversations cannot be read: " + err.Error()
	}
	if len(list) == 0 {
		return "nothing saved yet: a session saves as it goes"
	}
	this := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	rows := []string{fmt.Sprintf("%d conversations saved in %s · * is this project", len(list), filepath.Dir(path))}
	for _, c := range list {
		mark, own := " ", ""
		if c.Root == a.opts.Root {
			mark = "*"
		}
		if c.ID == this {
			own = " (this run)"
		}
		rows = append(rows, fmt.Sprintf(" %s %s%s  %s  %3d msgs  %s",
			mark, c.ID, own, c.Used.Format(sessionsWhenFmt), c.Messages, c.Root))
		rows = append(rows, "    "+oneLine(c.First, firstAskShown))
	}
	rows = append(rows, "come back to one by leaving: ykode --continue <id>, or --continue last")
	return strings.Join(rows, "\n") + "\n"
}
