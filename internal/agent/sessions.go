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

// ContinueFrom gives up the conversation this run is in and carries on with a saved one, for a front
// end that lets it be picked out of the list. Everything that follows is written to that conversation's
// file, measured with the byte ratio its own run learned and slotted in the server's cache under its
// id, so it reads as the conversation it was. The one this run was in is left on disk whole: it can be
// come back to the same way.
func (a *Agent) ContinueFrom(dir, id string) error {
	if dir == "" || id == "" {
		return fmt.Errorf("no saved conversation to continue")
	}
	if a.turnBusy() {
		// this replaces the whole conversation, which is what a request in flight is being built from
		return fmt.Errorf("a turn is running: continuing another conversation waits for it to finish (esc stops it)")
	}
	saved, err := session.LoadAll(filepath.Join(dir, id+".jsonl"), a.opts.Root)
	if err != nil {
		return err
	}
	if len(saved.Messages) == 0 {
		return fmt.Errorf("session: %s holds nothing to resume", id)
	}
	a.startOver()
	a.opts.Session = session.New(dir, id, a.opts.Root)
	a.client.Session = id          // the cache slot follows the conversation, or the next request pays for it again
	a.Learned(saved.BytesPerToken) // before the conversation is measured, so it is measured as it was
	a.Resume(saved.Messages)
	a.emit(Event{Kind: Cleared}) // what the server was doing describes the conversation that went
	return nil
}
