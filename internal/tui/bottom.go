package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	promptMark    = "❯ "
	maxInputLines = 10   // the input grows with what is typed, up to this
	maxInputRows  = 5000 // what the box holds at all, in wrapped rows: a pasted file still fits
	bottomGap     = ""   // a blank row so the cursor does not sit against the bottom of the terminal

	// reportRows is what the bottom holds under the box: the two report rows and the edge gap
	reportRows = 3
	boxEdges   = 2 // the input card's top and bottom edges
)

// keyGroup is one row of what ? shows: what the keys are for, then each key and what it does
type keyGroup struct {
	name string
	keys [][2]string
}

// keyGroups is what ? shows, grouped by what a reader is doing: writing, editing, reading, steering
var keyGroups = []keyGroup{
	{"prompt", [][2]string{{"enter", "send"}, {"ctrl+j alt/ctrl+enter", "newline"},
		{"tab", "complete a /command"}, {"ctrl+p ctrl+n", "earlier prompts"}}},
	{"edit", [][2]string{{"ctrl+a ctrl+e", "line start, end"}, {"ctrl+k", "kill the line"},
		{"ctrl+w", "kill a word"}, {"ctrl+v", "paste a picture"}}},
	{"scroll", [][2]string{{"↑ ↓ pgup pgdn", "scroll"}, {"ctrl+u ctrl+d", "half a page"},
		{"end", "back to the end"}, {"ctrl+r", "commands whole"}}},
	{"session", [][2]string{{"esc", "stop the run"}, {"ctrl+c", "stop the run, or quit"},
		{"ctrl+o", "fold the plan"}, {"ctrl+/", "this list"}}},
}

// keyLabels is the column the group names take, the longest name and a space
const keyLabels = len("session") + 2

// bottomArea is everything under the transcript: keys, queue, question, prompt card, then the report
func (m *model) bottomArea() string {
	var rows []string
	if m.showKeys {
		rows = append(rows, m.keysHelp())
	}
	rows = append(rows, m.planRows()...)
	rows = append(rows, m.waitingRows()...)
	// the candidates take the rows the screen can spare, less what stands above and follows them
	under := m.input.Height() + boxEdges + reportRows + blankRowsBelow // the box and the report are spaced apart
	if m.err != "" {
		under++
	}
	// a question takes the rows left after the session keeps its least, cutting choices to the screen
	ask := ""
	if len(m.pending) > 0 {
		ask = m.question(m.height - topRows - minTranscriptRows - len(rows) - under)
		under += lipgloss.Height(ask)
	}
	rows = append(rows, m.suggestRows(m.height-len(rows)-under-topRows-minTranscriptRows)...)
	if ask != "" {
		rows = append(rows, ask)
	}
	rows = append(rows, m.promptLines())
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

// waitingRows keep what was typed into a running turn, where a transcript notice would scroll off
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
	blankRowsBelow = 2 // the gap under the box and the one under the report
	askBar         = "▎ "
	askPreview     = 6  // lines of what a call would write or run that the question shows
	askChange      = 16 // rows of an edit the question shows: it sits above the prompt, which has to stay on the screen
)

// question is what a run waits on above the input, and the run is blocked until it is answered
func (m *model) question(screenRows int) string {
	if m.awaitingWords() {
		return m.modelQuestion(screenRows)
	}
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
		if change := askedChange(q.tool, q.args); change != "" {
			lines = append(lines, changeRows(change, askChange, room)...) // an edit is asked about as the change it is
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

// askBody is the part of a call the head squashes: a file's first lines, or the whole command
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

// askedChange is an edit's out and in read from its arguments, before the file has line numbers
func askedChange(tool, args string) string {
	if tool != "edit_file" {
		return ""
	}
	type edit struct {
		OldText   string  `json:"old_text"`
		NewText   *string `json:"new_text"`
		OldString string  `json:"old_string"`
		NewString *string `json:"new_string"`
	}
	var in struct {
		edit
		Edits []edit `json:"edits"`
	}
	if json.Unmarshal([]byte(args), &in) != nil {
		return ""
	}
	var change strings.Builder
	for _, e := range append([]edit{in.edit}, in.Edits...) {
		went, came := e.OldText+e.OldString, ""
		for _, text := range []*string{e.NewText, e.NewString} {
			if text != nil {
				came += *text
			}
		}
		if went == "" && came == "" {
			continue
		}
		change.WriteString("@@\n") // each edit is its own place in the file
		writeSide(&change, "-", went)
		writeSide(&change, "+", came)
	}
	return change.String()
}

// writeSide writes one side of an edit as the lines of a change.
func writeSide(change *strings.Builder, sign, text string) {
	if text == "" {
		return
	}
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		change.WriteString(sign + line + "\n")
	}
}

// promptLines is the input in its card, the mark on the first line; the edge marks the scroll
func (m *model) promptLines() string {
	mark := styleAccent.Render(promptMark)
	if m.running {
		mark = styleDim.Render(promptMark) // typing still works; it waits for the turn
	}
	lines := strings.Split(m.boxView(), "\n")
	for i := range lines {
		if i == 0 {
			lines[i] = mark + lines[i]
		} else {
			lines[i] = strings.Repeat(" ", lipgloss.Width(promptMark)) + lines[i]
		}
	}
	title := styleAccent.Render("ask")
	if m.running {
		title = styleWorking.Render("working")
	}
	label := ""
	if mark := m.scrollMark(); mark != "" {
		label = styleWarn.Render(mark)
	}
	return strings.Join(card(title, label, lines, m.width), "\n")
}

// boxLook is everything the typing box's drawing depends on, so an unchanged box is not drawn again
type boxLook struct {
	value                            string
	width, height, line, column, top int
	focused                          bool
	palette                          int
}

// boxView is the box as drawn: a streamed reply redraws the session thirty times a second, not the box
func (m *model) boxView() string {
	look := boxLook{value: m.input.Value(), width: m.input.Width(), height: m.input.Height(), line: m.input.Line(),
		column: m.input.Column(), top: m.input.ScrollYOffset(), focused: m.input.Focused(), palette: paletteGen}
	if look != m.boxLook || m.boxDrawn == "" {
		m.boxLook, m.boxDrawn = look, m.input.View()
	}
	return m.boxDrawn
}

// fitInput keeps the box, which grows by itself to ten rows, from pushing the session past its minimum
func (m *model) fitInput() {
	if room := max(m.height-topRows-m.otherRows()-minTranscriptRows, 1); m.input.Height() > room {
		m.input.SetHeight(room)
		m.bottom = "" // the box changed, so the rows under the session changed with it
	}
}

// otherRows is the bottom area apart from the box itself: the rule, the report, the gaps.
func (m *model) otherRows() int {
	return lipgloss.Height(m.screenBottom()) - m.input.Height()
}

// minTranscriptRows is the least the session keeps on screen however long the prompt is.
const minTranscriptRows = 3

// keysHelp lists the keys a group to a row, its name in a column; a narrow screen puts the name above
func (m *model) keysHelp() string {
	beside := m.width-keyLabels >= widestKey()
	var rows []string
	for _, g := range keyGroups {
		label := styleAccent.Render(g.name)
		if !beside {
			rows = append(rows, label)
			rows = append(rows, keyRows(g.keys, m.width)...)
			continue
		}
		for i, row := range keyRows(g.keys, m.width-keyLabels) {
			lead := strings.Repeat(" ", keyLabels)
			if i == 0 {
				lead = label + strings.Repeat(" ", keyLabels-len(g.name))
			}
			rows = append(rows, lead+row)
		}
	}
	return strings.Join(rows, "\n")
}

// keyRows lays entries in as few rows of width as they need, the same count on each, so none is a stub
func keyRows(keys [][2]string, width int) []string {
	lines := flowKeys(keys, width)
	per := (len(keys) + len(lines) - 1) / len(lines)
	var even [][][2]string
	for at := 0; at < len(keys); at += per {
		chunk := keys[at:min(at+per, len(keys))]
		if keysWidth(chunk) > width {
			even = lines // an even split would overflow a row: the plain flow stands
			break
		}
		even = append(even, chunk)
	}
	rows := make([]string, 0, len(even))
	for _, line := range even {
		row := ""
		for i, k := range line {
			if i > 0 {
				row += styleDim.Render(" · ")
			}
			row += styleKey.Render(k[0]) + " " + styleDim.Render(k[1])
		}
		rows = append(rows, row)
	}
	return rows
}

// flowKeys fills each row before starting the next, breaking between entries, never inside one
func flowKeys(keys [][2]string, width int) [][][2]string {
	lines := [][][2]string{nil}
	for _, k := range keys {
		last := len(lines) - 1
		if len(lines[last]) > 0 && keysWidth(append(lines[last][:len(lines[last]):len(lines[last])], k)) > width {
			lines = append(lines, nil)
			last++
		}
		lines[last] = append(lines[last], k)
	}
	return lines
}

// keysWidth is the columns a row of entries takes, counted as shown: the arrows take more bytes
func keysWidth(keys [][2]string) int {
	width := 3 * max(len(keys)-1, 0)
	for _, k := range keys {
		width += lipgloss.Width(k[0] + " " + k[1])
	}
	return width
}

// widestKey is the widest single entry, which the column beside the names must hold
func widestKey() int {
	widest := 0
	for _, g := range keyGroups {
		for _, k := range g.keys {
			widest = max(widest, lipgloss.Width(k[0]+" "+k[1]))
		}
	}
	return widest
}
