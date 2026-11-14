package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/audit"
	"github.com/yuriy-kovalchuk/ykode/internal/size"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// historyScreen is what /history answers with: every call this run made, newest first, narrowed by the
// same words the command takes, with enter reading one of them inside the box. A call is never written
// into the conversation - a diff and a wall of arguments belong to the screen, not to the transcript the
// model reads back.
type historyScreen struct {
	chooser
	history []audit.Entry
	root    string // what logged paths are shortened against: the project the run was started in
}

// openHistory puts the calls this run made on the screen. The words the command was given narrow the list
// from the start, and a call named by id opens already read in full.
func (m *model) openHistory(all bool, words, id string) {
	entries, unread, err := m.agent.Logged(all)
	s := &historyScreen{history: entries, root: m.cfg.Root, chooser: newChooser("history", "calls this run logged",
		"↑/↓ another call · enter reads it · / narrows · esc closes")}
	s.readHint = "↑/↓ scroll · pgup/pgdn jump · ←/→ another call · esc back to the list"
	s.detailRows = historyDetail
	if len(entries) == 0 {
		// there is nothing to look through, so there is nothing to open a box over
		m.log.notice("nothing has been logged yet: every call the model makes is logged")
		return
	}
	if unread > 0 {
		s.note = fmt.Sprintf("%d of the logged lines could not be read", unread)
	}
	if err != nil {
		s.note = "the log could not be read: " + err.Error()
	}
	s.load(s.historyRows())
	if words != "" {
		s.filter.SetValue(words) // the words the command was given narrow the list from the start
		s.filterRows()
	}
	if id != "" {
		for i, e := range s.rows {
			if e.id == id {
				s.at = i
				s.expanded = true // a call asked for by name is the thing the person wants to read
				break
			}
		}
	}
	s.filter.Focus()
	m.picker = s
}

// historyRows is one row per call: its id, the mark saying how it ended, when it ran, how long it took,
// and what it was asked to do in the words the tools use, since that is what a row has room for.
func (s *historyScreen) historyRows() []listRow {
	rows := make([]listRow, len(s.history))
	for i, e := range newestFirst(s.history) {
		rows[i] = listRow{
			id:   e.ID,
			mark: outcomeMark(e),
			lead: e.ID,
			text: historyWords(e),
			hay:  historyHay(e),
			cells: []listCell{
				{text: padTo(historyWhen(e.Time), 6)},
				{text: padTo(historyHow(e), 10), wide: true},
				{text: padTo(e.Tool, 14)},
			},
		}
	}
	return rows
}

// newestFirst puts the call that happened last on the first row, which is where a person looking for
// trouble starts reading.
func newestFirst(entries []audit.Entry) []audit.Entry {
	out := make([]audit.Entry, len(entries))
	for i, e := range entries {
		out[len(entries)-1-i] = e
	}
	return out
}

// historyWords is the call in the tools' own words: the command, the file and range, the pattern and
// where it was searched.
func historyWords(e audit.Entry) string {
	if words := tools.Describe(e.Tool, e.Args); words != "" {
		return words
	}
	return oneLine(e.Args, listRowMin)
}

// historyHay is what typing matches: the words of the call, plus the words /history itself takes, so
// "failed" and "outside" find the calls the command would have found.
func historyHay(e audit.Entry) string {
	words := []string{e.Tool, e.Args, e.Status, e.Decision, strings.Join(e.Paths, " ")}
	if e.Status != "ok" && e.Status != "started" {
		words = append(words, "failed")
	}
	if len(e.OutsideRoot) > 0 {
		words = append(words, "outside")
	}
	return strings.Join(words, " ")
}

// outcomeMark tells a call that worked from one that did not and one still running, so a glance at the
// list finds the trouble.
func outcomeMark(e audit.Entry) string {
	switch {
	case e.Status == "started":
		return "…"
	case e.Status == "ok":
		return "✓"
	}
	return "✗"
}

// historyWhen is a time a person reads as a moment, not a timestamp.
func historyWhen(t time.Time) string { return t.Format("15:04") }

// historyHow is how long the call took, and the code it came back with when it ran something: the mark on
// the row already says whether it worked, so this column only adds what the mark cannot say.
func historyHow(e audit.Entry) string {
	if e.Status == "started" {
		return "running"
	}
	how := fmt.Sprintf("%dms", e.DurationMS)
	if e.ExitCode != nil {
		how += fmt.Sprintf(" exit %d", *e.ExitCode)
	}
	return how
}

// keys: the list's keys, with enter reading the call under the cursor inside the box rather than handing
// it to the transcript.
func (s *historyScreen) keys(msg tea.Msg) (tea.Cmd, overlayNext, bool) {
	return s.chooser.keys(msg, func(string) (tea.Cmd, overlayNext) {
		s.expanded = true // the pane below takes the whole call; the list keeps the row it is about
		return nil, stayOpen
	})
}

// view draws the list, or the one call scrolled through the box when the cursor has been asked to read it.
func (s *historyScreen) view(width, height int) string {
	return s.chooser.view(width, height, s.historyDetail)
}

// historyDetail is what the box says about a call: read from the list, the short reading - what it was
// asked for, since that is the part the row has no room for, and the rest of it on one row after. Read in
// full, everything about the call there is to know, which is more than a box holds and is scrolled through.
func (s *historyScreen) historyDetail(id string) []string {
	e := s.entry(id)
	if e == nil {
		return nil
	}
	if s.expanded {
		return s.readCall(*e)
	}
	lines := audit.AskedFor(e.Args)
	if len(lines) == 0 {
		lines = []string{"no arguments"}
	}
	return append(lines, styleDim.Render(historyHead(*e)))
}

// readCall is the whole call: its arguments as the model wrote them, every line of them, the files it
// touched, what it changed and what it came back with.
func (s *historyScreen) readCall(e audit.Entry) []string {
	lines := []string{setupTitle.Render(historyHead(e))}
	if asked := audit.WholeArgs(e.Args); len(asked) > 0 {
		lines = append(lines, styleDim.Render("asked for:"), "")
		lines = append(lines, asked...)
	} else {
		lines = append(lines, styleDim.Render("no arguments"))
	}
	if len(e.Paths) > 0 {
		lines = append(lines, "", styleDim.Render("files:"), strings.Join(shortenAll(e.Paths, s.root), "\n"))
	}
	if len(e.OutsideRoot) > 0 {
		lines = append(lines, "", styleWarn.Render("outside the project: "+strings.Join(shortenAll(e.OutsideRoot, s.root), ", ")))
	}
	if e.Diff != "" {
		lines = append(lines, "", styleDim.Render("what it changed:"), e.Diff)
	}
	lines = append(lines, "", styleDim.Render(historyCameBack(e)))
	if e.OutputPath != "" {
		lines = append(lines, styleDim.Render("kept whole in "+filepath.Base(e.OutputPath)))
	}
	return lines
}

// historyHead is the call's identity on one row: what ran, who allowed it, how it ended, how long it took.
func historyHead(e audit.Entry) string {
	return fmt.Sprintf("%s %s · allowed by %s · %s · %s", e.ID, e.Tool, e.Decision, historyState(e), historyHow(e))
}

func historyState(e audit.Entry) string {
	switch e.Status {
	case "started":
		return "running"
	case "ok":
		return "done"
	default:
		return e.Status
	}
}

// historyCameBack is what the call produced, in the terms the transcript uses for it.
func historyCameBack(e audit.Entry) string {
	if e.OutputPath != "" {
		return fmt.Sprintf("came back with %s, kept whole in %s", size.Text(e.ResultBytes), filepath.Base(e.OutputPath))
	}
	return fmt.Sprintf("came back with %s", size.Text(e.ResultBytes))
}

// shortenAll makes a list of paths read as project paths, which is how a person looking at the screen
// thinks about them.
func shortenAll(paths []string, root string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = shorten(p, root)
	}
	return out
}

// entry finds the call the cursor is on, by the id the row carries.
func (s *historyScreen) entry(id string) *audit.Entry {
	for i, e := range s.history {
		if e.ID == id {
			return &s.history[i]
		}
	}
	return nil
}

// historyDetail is how many rows the pane keeps for the call the cursor is on.
const historyDetail = 6
