package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	promptMark    = "❯ "
	maxInputLines = 10 // the input grows with what is typed, up to this
	inputGap      = "" // a blank row between the prompt and what the session reports
	bottomGap     = "" // a blank row so the cursor does not sit against the bottom of the terminal
)

// keys is what ? shows, as key and what it does.
var keys = [][2]string{
	{"enter", "send"}, {"ctrl+j", "newline"}, {"ctrl+p ctrl+n", "earlier prompts"},
	{"tab", "complete a /command"},
	{"↑ ↓ pgup pgdn", "scroll"}, {"ctrl+u ctrl+d", "half a page"}, {"end", "back to the end"},
	{"esc", "stop the run"}, {"ctrl+t", "terminal selects"}, {"ctrl+o", "fold the plan"},
	{"ctrl+c", "quit, stop mid-run"}, {"ctrl+/", "this list"},
}

// bottomArea is everything under the transcript: the rule, the keys if ? asked for them, what is
// waiting or being chosen, the question if one waits, the prompt, and under it the two rows the
// session reports on. The report goes last: it is read after the prompt, not before it.
func (m *model) bottomArea() string {
	rows := []string{ruleWith(m.width, m.scrollMark())}
	if m.showKeys {
		rows = append(rows, m.keysHelp())
	}
	rows = append(rows, m.planRows()...)
	rows = append(rows, m.waitingRows()...)
	rows = append(rows, m.suggestRows()...)
	if len(m.pending) > 0 {
		rows = append(rows, m.question())
	}
	rows = append(rows, m.promptLines(), inputGap)
	s := m.status()
	rows = append(rows, sessionLine(s), statusLine(s))
	if m.err != "" {
		rows = append(rows, styleBad.Render(oneLine(m.err, max(m.width-2, 1))))
	}
	return strings.Join(append(rows, bottomGap), "\n")
}

const (
	waitingMark = "… " // an ask that has not reached the model yet
	waitingLead = "waits for the next round-trip: "
)

// waitingRows keep what the user typed into a running turn on the screen: a notice in the
// transcript scrolls away with the session, and an ask that has not been answered has not stopped
// being relevant.
func (m *model) waitingRows() []string {
	var rows []string
	for _, ask := range m.agent.Waiting() {
		room := m.width - lipgloss.Width("  "+waitingMark+waitingLead) - 2
		rows = append(rows, "  "+styleDim.Render(waitingMark+waitingLead)+oneLine(ask, max(room, 1)))
	}
	return rows
}

// askBar runs down the left of a question, so it reads as one thing apart from the prompt under it.
const (
	askBar     = "▎ "
	askPreview = 6 // lines of what a call would write or run that the question shows
)

// question is the confirmation a run waits on, above the input: what is asked in a bar of its own,
// the call in the words a person reads, a few lines of what it would write or run, and the keys.
func (m *model) question() string {
	q := m.pending[0]
	room := max(m.width-lipgloss.Width(askBar)-2, 10)
	var lines []string
	if q.tool == "" {
		lines = append(lines, styleWarn.Render("⚠ ")+oneLine(q.text, room))
	} else {
		lines = append(lines, styleWarn.Render("⚠ ")+styleBold.Render(q.tool)+styleDim.Render("  allow this call?"))
		if what := described(q.tool, q.args, m.root); what != "" {
			lines = append(lines, cutToWidth(what, room))
		}
		for _, line := range askBody(q.tool, q.args, room) {
			lines = append(lines, styleDim.Render("┊ ")+styleDim.Render(line))
		}
	}
	if len(q.target) > 0 {
		lines = append(lines, styleWarn.Render(cutToWidth("outside the project: "+strings.Join(q.target, ", "), room)))
	}
	answers := styleKey.Render("y") + styleDim.Render(" yes · ") + styleKey.Render("a") + styleDim.Render(" always · ") +
		styleKey.Render("n") + styleDim.Render(" no")
	if len(m.pending) > 1 {
		answers += styleDim.Render(fmt.Sprintf(" · %d more waiting", len(m.pending)-1))
	}
	lines = append(lines, answers)
	bar := styleWarn.Render(askBar)
	for i, line := range lines {
		lines[i] = bar + line
	}
	return strings.Join(lines, "\n")
}

// askBody is the part of a call the question shows beyond its head: the first lines of a file about to
// be written, or the whole command about to run, which the head squashes onto one line.
func askBody(tool, args string, width int) []string {
	var in struct {
		Content string `json:"content"`
		Command string `json:"command"`
	}
	if json.Unmarshal([]byte(args), &in) != nil {
		return nil
	}
	var text string
	switch tool {
	case "write_file":
		text = in.Content
	case "exec_command":
		if !strings.Contains(strings.TrimSpace(in.Command), "\n") {
			return nil // the head already says a one line command whole
		}
		text = in.Command
	default:
		return nil
	}
	all := strings.Split(strings.TrimRight(text, "\n"), "\n")
	rows := make([]string, 0, askPreview+1)
	for _, line := range all[:min(len(all), askPreview)] {
		rows = append(rows, cutToWidth(diffText(line), width-2))
	}
	if len(all) > askPreview {
		rows = append(rows, fmt.Sprintf("… %d more lines", len(all)-askPreview))
	}
	return rows
}

// promptLines is the input with the prompt mark on its first line.
func (m *model) promptLines() string {
	mark := styleAccent.Render(promptMark)
	if m.running {
		mark = styleDim.Render(promptMark) // typing still works; it waits for the turn
	}
	lines := strings.Split(m.input.View(), "\n")
	for i := range lines {
		if i == 0 {
			lines[i] = mark + lines[i]
		} else {
			lines[i] = strings.Repeat(" ", lipgloss.Width(promptMark)) + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// fitInput grows the box with the rows typed, to ten, and never past the point where the session
// would have too few rows left to read.
func (m *model) fitInput() {
	rows := min(inputRows(m.input.Value(), m.input.Width()), maxInputLines)
	if room := m.height - m.otherRows() - minTranscriptRows; rows > room {
		rows = max(room, 1)
	}
	if rows != m.input.Height() {
		m.input.SetHeight(rows)
		m.bottom = "" // the box changed, so the rows under the session changed with it
	}
}

// otherRows is the bottom area apart from the box itself: the rule, the report, the gaps.
func (m *model) otherRows() int {
	return lipgloss.Height(m.screenBottom()) - m.input.Height()
}

// inputRows counts the rows the text takes once the box has wrapped it: a long line typed without
// a return is not one row on the screen.
func inputRows(text string, width int) int {
	rows := 0
	for _, line := range strings.Split(text, "\n") {
		rows += len(wrap(line, width))
	}
	return max(rows, 1)
}

// minTranscriptRows is the least the session keeps on screen however long the prompt is.
const minTranscriptRows = 3

// keysHelp lists the keys, wrapping between entries so no row is wider than the screen.
func (m *model) keysHelp() string {
	var rows []string
	row, used := "", 0
	for _, k := range keys {
		desc := k[1]
		if k[0] == "ctrl+t" && m.mouseOn {
			desc = "give it back"
		}
		plain := k[0] + " " + desc
		if used > 0 && used+3+lipgloss.Width(plain) > m.width {
			rows, row, used = append(rows, row), "", 0
		}
		if used > 0 {
			row += styleDim.Render(" · ")
			used += 3
		}
		row += styleKey.Render(k[0]) + " " + styleDim.Render(desc)
		used += lipgloss.Width(plain) // the arrows take more bytes than they do columns
	}
	return strings.Join(append(rows, row), "\n")
}
