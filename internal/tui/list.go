package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// the lists that answer commands share filter, cursor, row window and box, held here once

// chooser is such a list: the rows typing leaves, where the cursor is, and what was refused.
type chooser struct {
	title, subtitle string
	hint            string // what the keys do while the list is looked through
	readHint        string // what they do while one entry is being read in full
	filter          textinput.Model
	all, rows       []listRow
	at, top, room   int
	inner           int    // what the last view had to draw in, so a row can be worded to fit it
	chosen          string // the row the pane is about, once the list has been looked at
	note            string // said below the list instead of in the transcript, because the screen answers the command

	// detailRows is how many rows the pane under the list keeps, by what the list has to say
	detailRows int

	// expanded reads one entry in full: the list falls back to its row, the box scrolls the entry
	expanded bool
	scroll   viewport.Model // the entry read in full, scrolled through rather than cut
}

// listRow is one entry: id, kind, a few fixed-width facts, and what it was about
type listRow struct {
	id, mark, lead, text, hay string
	cells                     []listCell
}

// listCell is one of those facts. wide says a narrow terminal gives it up: the pane below says the same.
type listCell struct {
	text string
	wide bool
}

const (
	listLead   = "  "
	listLeadW  = 5  // the column every list starts its facts in
	listDetail = 4  // rows the details pane keeps, when the terminal has them
	listWide   = 72 // the terminal width from which the wide cells are shown
	listRowMin = 40 // the fewest characters a row keeps for what it was about
	listMinRow = 1  // the fewest rows a list shows before the box is pointless
	listFloor  = 2  // rows a list keeps when there are that many to show, however short the terminal
	listNarrow = 1  // the fewest rows the details pane keeps when the terminal is short
	listChrome = 8  // rows the box spends on itself: the frame, the title, the filter, the hint
)

// newChooser is an empty list with its filter ready to be typed into.
func newChooser(title, subtitle, hint string) chooser {
	in := textinput.New()
	in.Prompt = "  "
	in.Placeholder = "type to narrow"
	in.Focus()
	return chooser{title: title, subtitle: subtitle, hint: hint, filter: in, detailRows: listDetail, scroll: viewport.New(1, 1)}
}

// load puts the rows in: all of them, and what the filter leaves, which is everything to start with.
func (c *chooser) load(rows []listRow) {
	c.all, c.rows = rows, append([]listRow(nil), rows...)
	c.at = min(c.at, max(len(c.rows)-1, 0))
}

// keys answers every list alike: esc closes, arrows move, typing narrows, enter chooses
func (c *chooser) keys(msg tea.Msg, choose func(id string) (tea.Cmd, overlayNext)) (tea.Cmd, overlayNext, bool) {
	key, isKey := msg.(tea.KeyMsg)
	if !isKey {
		return nil, stayOpen, false
	}
	if c.expanded {
		return c.readKeys(msg)
	}
	switch key.String() {
	case "esc", "ctrl+c":
		return nil, closeOverlay, true
	case "up", "k":
		c.move(-1)
	case "down", "j":
		c.move(1)
	case "pgup", "home", "g":
		c.top, c.at = 0, 0
	case "pgdown", "end", "G":
		if len(c.rows) > 0 {
			c.top, c.at = 0, len(c.rows)-1
		}
	case "enter":
		if len(c.rows) == 0 {
			return nil, stayOpen, true
		}
		c.chosen = c.rows[c.at].id
		cmd, next := choose(c.chosen)
		return cmd, next, true
	default:
		// the filter box takes the rest: typing narrows, backspace widens again
		var cmd tea.Cmd
		c.filter, cmd = c.filter.Update(msg)
		c.filterRows()
		return cmd, stayOpen, true
	}
	return nil, stayOpen, true
}

// readKeys scrolls one entry: the side arrows step to the next entry, esc goes back to the list
func (c *chooser) readKeys(msg tea.Msg) (tea.Cmd, overlayNext, bool) {
	if key, isKey := msg.(tea.KeyMsg); isKey {
		switch key.String() {
		case "esc", "ctrl+c":
			c.expanded = false
			return nil, stayOpen, true
		case "left", "h":
			c.step(-1)
			return nil, stayOpen, true
		case "right", "l":
			c.step(1)
			return nil, stayOpen, true
		case "enter": // the same way in: back to the list
			c.expanded = false
			return nil, stayOpen, true
		case "home":
			c.scroll.SetYOffset(0)
			return nil, stayOpen, true
		case "end": // the last row of the entry: what it ended with is often what matters
			c.scroll.SetYOffset(c.scroll.TotalLineCount())
			return nil, stayOpen, true
		}
	}
	var cmd tea.Cmd
	c.scroll, cmd = c.scroll.Update(msg)
	return cmd, stayOpen, true
}

// step moves to the entry before or after the one being read, and starts it from its top.
func (c *chooser) step(by int) {
	c.move(by)
	c.scroll.SetYOffset(0)
}

// filterRows is what typing leaves, and keeps the cursor on the screen.
func (c *chooser) filterRows() {
	needle := strings.ToLower(strings.TrimSpace(c.filter.Value()))
	c.rows = c.rows[:0]
	for _, r := range c.all {
		if needle == "" || strings.Contains(strings.ToLower(r.id+" "+r.hay), needle) {
			c.rows = append(c.rows, r)
		}
	}
	c.at = min(c.at, max(len(c.rows)-1, 0))
	c.clamp()
}

func (c *chooser) move(by int) {
	c.at = min(max(c.at+by, 0), max(len(c.rows)-1, 0))
	c.clamp()
}

// clamp keeps the cursor inside the rows there is room for: the box is drawn over, not scrolled
func (c *chooser) clamp() {
	if c.at < c.top {
		c.top = c.at
	} else if c.room > 0 && c.at >= c.top+c.room {
		c.top = c.at - c.room + 1
	}
}

// shown is the row the pane is about: the cursor's row, once it has been moved on purpose
func (c *chooser) shown() string {
	if len(c.rows) == 0 {
		return ""
	}
	return c.rows[c.at].id // the pane follows the cursor, so the arrows walk the entries one after another
}

// view is the list with a details pane below it, one height whatever the pane has to say
func (c *chooser) view(width, height int, detail func(id string) []string) string {
	inner := overlayInner(width)
	c.inner = inner
	c.filter.Width = max(inner-4, 1)
	// what the box has left is shared two thirds to the pane, the list keeping two rows when it can
	spare := height - listChrome - listFloor
	details := min(c.detailRows, max(spare*2/3, listNarrow))
	if c.expanded {
		// reading one entry: a row says which it is and the rest of the box is the entry, scrolled
		details = max(height-listChrome-listFloor, listNarrow)
	}
	c.room = max(height-listChrome-details, listMinRow)
	c.room = min(c.room, len(c.rows))
	more := len(c.rows) > c.room
	if more && !c.expanded {
		c.room = max(c.room-1, listMinRow) // one row of the window says there is more to come
	}
	c.clamp()

	var list strings.Builder
	if len(c.rows) == 0 {
		list.WriteString("  " + styleDim.Render("nothing matches "+c.filter.Value()))
	}
	for i := c.top; i < min(c.top+c.room, len(c.rows)); i++ {
		list.WriteString("\n" + c.line(c.rows[i], i == c.at, inner))
	}
	if rest := len(c.rows) - (c.top + c.room); more && rest > 0 {
		list.WriteString("\n" + styleDim.Render(fmt.Sprintf("    … %d more, ↓", rest)))
	}

	title := "ykode · " + c.title
	head := setupTitle.Render(title) + setupDim.Render(cutToWidth(
		fmt.Sprintf("   %d %s", len(c.rows), c.subtitle), inner-lipgloss.Width(title)))
	lines := []string{head, listLead + c.filter.View(), list.String(),
		c.reading(detail, inner, details), setupDim.Render(shortenHint(c.hintWith(c.scroll), inner))}
	return place(width, height, strings.Join(lines, "\n"))
}

// reading is the pane below the list: a few rows, or the whole entry scrolled through
func (c *chooser) reading(detail func(id string) []string, inner, keep int) string {
	if !c.expanded {
		return c.pane(detail, inner, keep)
	}
	c.scroll.Width = inner
	c.scroll.Height = keep
	var body []string
	for _, line := range detail(c.shown()) {
		body = append(body, wrapKept(line, inner)...)
	}
	if c.note != "" {
		body = append(body, styleWarn.Render(c.note))
	}
	c.scroll.SetContent(strings.Join(body, "\n"))
	return c.scroll.View()
}

// hintWith says what the keys do, and how much of an entry is still below the box
func (c *chooser) hintWith(box viewport.Model) string {
	hint := c.hint
	if c.expanded && c.readHint != "" {
		hint = c.readHint
	} else if !c.expanded {
		return hint
	}
	if rest := box.TotalLineCount() - box.YOffset - box.Height; rest > 0 {
		hint += fmt.Sprintf(" · %d rows more", rest)
	}
	return hint
}

// line is one row: the cursor, what kind of entry it is, its facts in columns, and what it was about.
func (c *chooser) line(r listRow, on bool, width int) string {
	style, cursor := styleDim, " "
	if on {
		style, cursor = styleBold, setupPick.Render("❯")
	}
	head := listLead + cursor + " " + orSpace(r.mark)
	if r.lead != "" {
		head += " " + styleDim.Render(padTo(r.lead, listLeadW))
	}
	for _, cell := range r.cells {
		if cell.wide && width < listWide {
			continue // a short terminal reads this from the pane instead
		}
		head += " " + styleDim.Render(cell.text)
	}
	room := max(width-lipgloss.Width(head)-1, listRowMin) // the row's own words take whatever the columns leave
	return cutToWidth(head+" "+style.Render(oneLine(r.text, room)), width)
}

// pane is what the list knows about the chosen row, cut to the rows the terminal has for it.
func (c *chooser) pane(detail func(id string) []string, inner, keep int) string {
	lines := detail(c.shown())
	if c.note != "" {
		lines = append(lines, styleWarn.Render(c.note))
	}
	rows := []string{}
	for _, line := range lines {
		// wrapKept, not wrap: a value over one row hangs under itself instead of starting anew
		rows = append(rows, wrapKept(line, inner)...)
		if len(rows) >= keep {
			break
		}
	}
	if len(rows) > keep {
		// the pane ran out of rows mid-sentence: say so, rather than end where the terminal did
		rows[keep-1] = styleDim.Render(strings.TrimRight(rows[keep-1], " ") + " …")
		rows = rows[:keep]
	}
	return strings.Join(rows, "\n")
}

// shortenHint drops the guessable keys from the front, so a narrow screen says how to leave
func shortenHint(full string, inner int) string {
	parts := strings.Split(full, " · ")
	for i := range parts {
		if hint := strings.Join(parts[i:], " · "); lipgloss.Width(hint) <= inner {
			return hint
		}
	}
	return parts[len(parts)-1]
}

func orSpace(mark string) string {
	if mark == "" {
		return " "
	}
	return mark
}
