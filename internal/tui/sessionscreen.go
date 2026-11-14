package tui

import (
	"fmt"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/session"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// sessionsScreen is /sessions: the conversations this project has saved, and putting one of them in this
// run's place. The list, the filter and the box are the screen's shared ones; what a row says, what the
// pane says about one and what choosing it does are this command's.
type sessionsScreen struct {
	chooser
	conversations []session.Conversation
	root          string // which project's conversations this run may take over
	current       string // the conversation this run is in, so the marks do not lie after a switch
	now           time.Time
	take          func(id string) error // what the run does with a chosen conversation: continue it
}

// sessionLimit is how many saved conversations the list reads. They come newest first, so this is the
// oldest one the screen can reach; --continue still takes an id typed by hand.
const sessionLimit = 100

// openSessions puts the saved conversations on the screen. The list is read once, when the screen opens:
// nothing else saves a conversation while it is up.
func (m *model) openSessions() {
	list, err := session.Describe(m.cfg.SessionDir, sessionLimit)
	if err != nil {
		m.log.notice("sessions: " + err.Error())
		return
	}
	if len(list) == 0 {
		m.log.notice("nothing saved yet for this project: a conversation starts being saved at its first message")
		return
	}
	s := &sessionsScreen{
		chooser: newChooser("sessions", fmt.Sprintf("conversations in %s", shortenHome(m.cfg.SessionDir)),
			"enter continues it here · type to narrow · ↑/↓ moves · esc closes"),
		conversations: list,
		root:          m.cfg.Root,
		current:       m.cfg.Session,
		now:           time.Now(),
		take:          m.continueWith,
	}
	s.load(s.sessionRows())
	m.picker = s
}

// keys is the list's, with this screen's choice in it.
func (s *sessionsScreen) keys(msg tea.Msg) (tea.Cmd, overlayNext, bool) {
	return s.chooser.keys(msg, s.pick)
}

// view is the list with the chosen conversation's details below it.
func (s *sessionsScreen) view(width, height int) string {
	return s.chooser.view(width, height, s.sessionDetail)
}

// sessionRows are the conversations in date order, each with when it was written, how much is in it,
// which project it belongs to and what it started with.
func (s *sessionsScreen) sessionRows() []listRow {
	rows := make([]listRow, len(s.conversations))
	for i, c := range s.conversations {
		rows[i] = listRow{
			id:   c.ID,
			mark: s.markFor(c),
			lead: relTime(s.now.Sub(c.Used)),
			text: c.First,
			hay:  c.Root + " " + c.First,
			cells: []listCell{{text: padTo(fmt.Sprintf("%d msgs", c.Messages), 9)},
				{text: padTo(trimProject(c.Root), 12), wide: true}},
		}
	}
	return rows
}

// markFor tells this run's, this project's, and another project's conversation apart in one cell.
func (s *sessionsScreen) markFor(c session.Conversation) string {
	switch {
	case c.ID == s.current:
		return "@"
	case c.Root != s.root:
		return "·"
	}
	return "*"
}

// sessionHead says which conversation the pane is about and when it was last written: the whole date when
// there is room for it, an age when there is not - this is the one row that says whether the conversation
// is from today or from before the machine was rebuilt.
func (s *sessionsScreen) sessionHead(c session.Conversation) string {
	full := fmt.Sprintf("%s · %d messages · last written %s", c.ID, c.Messages, c.Used.Format("02 Jan 2006 15:04"))
	if s.inner < listWide {
		return fmt.Sprintf("%s · %d msgs · %s", c.ID, c.Messages, relTime(s.now.Sub(c.Used)))
	}
	return full
}

// sessionDetail is the conversation the cursor is on: its id to continue it by hand, when and where it was
// written, and the ask it started with.
func (s *sessionsScreen) sessionDetail(id string) []string {
	c := s.conversation(id)
	if c.ID == "" {
		return nil
	}
	lines := []string{s.sessionHead(c)}
	lines = append(lines, shortenHome(c.Root))
	switch {
	case c.ID == s.current:
		lines = append(lines, styleWarn.Render("this is the conversation this run is in"))
	case c.Root != s.root:
		lines = append(lines, styleWarn.Render("ykode --continue "+c.ID+" --root "+c.Root))
	}
	if c.First != "" {
		lines = append(lines, styleDim.Render(c.First))
	}
	return lines
}

func (s *sessionsScreen) conversation(id string) session.Conversation {
	for _, c := range s.conversations {
		if c.ID == id {
			return c
		}
	}
	return session.Conversation{}
}

// pick puts the chosen conversation in this run's place, or leaves the reason on the box: a conversation
// written in another project is only read back by that project, and this run's own has nowhere to go.
func (s *sessionsScreen) pick(id string) (tea.Cmd, overlayNext) {
	c := s.conversation(id)
	switch {
	case id == s.current:
		s.note = "this is the conversation this run is in"
		return nil, stayOpen
	case c.Root != s.root:
		s.note = "that conversation belongs to another project: continue it there"
		return nil, stayOpen
	}
	if err := s.take(id); err != nil {
		s.note = "that conversation could not be continued: " + err.Error()
		return nil, stayOpen
	}
	return nil, closeOverlay
}

// continueWith takes a saved conversation over as this run's: the model reads it, the screen draws it,
// and what comes next is saved to its file. The conversation the run gave up is left whole, so it can be
// picked out of the list again.
func (m *model) continueWith(id string) error {
	// the folder of shortened tool results is named for the conversation, as the run's is
	where := filepath.Join(filepath.Dir(m.cfg.ResultDir), id)
	if err := m.agent.ContinueFrom(m.cfg.SessionDir, id, tools.NewResultStore(where)); err != nil {
		return err
	}
	m.cfg.Session, m.cfg.SessionFile, m.cfg.ResultDir = id, m.agent.SessionFile(), where

	// the screen holds the conversation that was given up, and the walk-back holds its asks
	m.log.forget()
	asks := resume(&m.log, id, m.agent.Conversation())
	m.history, m.histIdx = asks, len(asks)
	m.log.notice("what this run was doing stays saved: it is in the list again")
	return nil
}

// trimProject keeps the last part of a project path: the whole path is in the pane, and a row has room
// for the conversation, not for where it lives three levels deep.
func trimProject(root string) string {
	if base := filepath.Base(root); base != "." && base != "/" && base != root {
		return base
	}
	return shortenHome(root)
}

// relTime is when a conversation was last written, close enough to say which one it was: minutes and
// hours while they are recent, days after that.
func relTime(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// pickerMsg gives the list its key and closes it when it is done.
func (m *model) pickerMsg(msg tea.Msg) (tea.Cmd, bool) {
	cmd, next, taken := m.picker.keys(msg)
	if !taken {
		return nil, false
	}
	if next == closeOverlay {
		m.picker = nil
		m.refresh()
	}
	return cmd, true
}
