package tui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/yuriy-kovalchuk/ykode/internal/size"
)

const (
	draftPreviewLines = 8    // the newest lines of a call being written that the screen shows
	draftTailBytes    = 4096 // decode only this much of the end: a 100 KB file must not slow every token
	draftLookBytes    = 512  // a key is hunted this often as the arguments grow, not once per token
)

// draftBody names the argument each tool's preview shows; other tools show their raw arguments.
var draftBody = map[string]string{"write_file": "content", "edit_file": "new_text", "exec_command": "command"}

// draftState is what the screen has read of a call's arguments while the model writes them. The
// arguments only grow, so each read starts where the last one stopped: a file of a hundred kilobytes
// is read once as it is written, not again for every redraw.
type draftState struct {
	path    string
	pathEnd int // where the path's text ends in args; -1 while the model has not finished it
	bodyAt  int // where the text the preview shows begins; -1 until the model reached it
	bodyEnd int // where that text ends; -1 while it is still being written
	seen    int // how far of the body has been walked
	lines   int // line breaks walked so far
	hunted  int // how long args was when the keys were last looked for
}

// newDraftState starts unset rather than at zero: zero would read as "the text starts at the top".
func newDraftState() draftState {
	return draftState{pathEnd: -1, bodyAt: -1, bodyEnd: -1}
}

// draft is a tool call the model is still writing, as the screen shows it.
type draft struct {
	call  int
	tool  string
	args  string
	state draftState
}

// drafted reads whatever has been written since the last look, in place on the entry, and shows the
// call: its path, how much of it is there, and the newest lines.
func drafted(e *entry) draft {
	readDraft(&e.state, e.tool, e.args)
	return draft{call: e.callID, tool: e.tool, args: e.args, state: e.state}
}

// readDraft keeps the reading of a call's arguments up to date without reading them again.
func readDraft(st *draftState, tool, args string) {
	// the path comes first in the arguments: it is looked for while they are short, then settled, so
	// what the model writes later cannot be mistaken for it
	if st.pathEnd < 0 && len(args) <= draftLookBytes {
		if at, ok := valueStart(args, "path"); ok {
			if end := closingQuote(args[at:]); end >= 0 {
				st.path, st.pathEnd = unescape(args[at:at+end]), at+end
			}
		}
	}
	key, known := draftBody[tool]
	if !known {
		return
	}
	// finished text the model has moved past: a second edit, another field. The newest occurrence is
	// the one being written now.
	movedOn := st.bodyEnd >= 0 && len(args) > st.bodyEnd
	if st.bodyAt < 0 || movedOn {
		if !huntIsDue(args, st.hunted) {
			return
		}
		st.hunted = len(args)
		at, found := valueStart(args, key)
		if !found {
			return
		}
		if at != st.bodyAt {
			st.bodyAt, st.bodyEnd, st.seen, st.lines = at, -1, at, 0
		}
	}
	if st.bodyEnd >= 0 {
		return // what is written is written
	}
	from := max(st.seen, st.bodyAt)
	if from > st.bodyAt && args[from-1] == '\\' {
		from-- // a line break split between two pieces is counted once, in the piece that finishes it
	}
	breaks, quote := scanValue(args[from:])
	st.lines += breaks
	st.seen = len(args)
	if quote >= 0 {
		st.bodyEnd = from + quote
	}
}

// huntIsDue says whether the arguments have grown enough to be worth reading again in search of a
// key. Half a kilobyte of new arguments at a time is enough: a key that was not in the newest part
// is not about to turn up at the end of a file being written.
func huntIsDue(args string, hunted int) bool {
	return len(args) <= draftLookBytes || len(args) >= hunted+draftLookBytes
}

// scanValue counts the escaped line breaks in a piece of a JSON string and says where its closing
// quote is, or -1 while the model is still writing the string.
func scanValue(piece string) (breaks, quote int) {
	for i := 0; i < len(piece); i++ {
		switch piece[i] {
		case '\\':
			if i+1 < len(piece) && piece[i+1] == 'n' {
				breaks++
			}
			i++
		case '"':
			return breaks, i
		}
	}
	return breaks, -1
}

// target is the path the call names: settled once, before the file's own text begins.
func (d draft) target() string { return d.state.path }

// preview is the newest lines of what the call writes, and how many lines and bytes it holds so far.
func (d draft) preview() (lines []string, total, bytes int) {
	if _, known := draftBody[d.tool]; !known {
		return tail(d.args), lineCount(d.args, "\n"), len(d.args)
	}
	if d.state.bodyAt < 0 {
		return nil, 0, 0
	}
	raw := d.args[d.state.bodyAt:]
	if d.state.bodyEnd >= 0 {
		raw = raw[:d.state.bodyEnd-d.state.bodyAt]
	}
	if raw == "" {
		return nil, 0, 0
	}
	total = d.state.lines + 1 // the line being written counts, an editor's rule
	if strings.HasSuffix(raw, `\n`) {
		total = d.state.lines
	}
	return tail(shownTail(raw)), total, len(raw)
}

// shownTail decodes the end of the text being written: the beginning of a big file is off the screen
// either way, and decoding it for every redraw costs the whole file.
func shownTail(raw string) string {
	from := raw
	if len(raw) > draftTailBytes {
		from = raw[len(raw)-draftTailBytes:]
		// start after an escaped newline, so decoding never begins inside an escape
		if nl := strings.Index(from, `\n`); nl >= 0 {
			from = from[nl+2:]
		}
	}
	return unescape(from)
}

// lineCount counts lines the way an editor does: a closing newline does not start another one.
func lineCount(text, newline string) int {
	n := strings.Count(text, newline)
	if !strings.HasSuffix(text, newline) {
		n++
	}
	return n
}

// draftHead is the one-line head of a call being written: its target and how much is written so
// far. It reports numbers the preview already counted, since a call of a hundred kilobytes is
// redrawn while it grows.
func draftHead(target string, lines, bytes int) string {
	parts := []string{}
	if target != "" {
		parts = append(parts, target)
	}
	if bytes > 0 {
		parts = append(parts, size.Text(bytes), fmt.Sprintf("%d lines", lines))
	}
	return strings.Join(parts, " · ")
}

func tail(text string) []string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > draftPreviewLines {
		lines = lines[len(lines)-draftPreviewLines:]
	}
	return lines
}

// valueStart is where a string field's text begins in arguments that may stop anywhere. Everything
// it drops is a prefix, so what is left keeps its place in args.
func valueStart(args, key string) (int, bool) {
	at := strings.LastIndex(args, `"`+key+`"`)
	if at < 0 {
		return 0, false
	}
	rest := args[at+len(key)+2:]
	if !strings.HasPrefix(rest, ":") {
		rest = strings.TrimLeft(rest, " \t\n\r")
	}
	if !strings.HasPrefix(rest, ":") {
		return 0, false
	}
	rest = strings.TrimLeft(rest[1:], " \t\n\r")
	if !strings.HasPrefix(rest, `"`) {
		return 0, false
	}
	return len(args) - len(rest) + 1, true
}

// closingQuote is where an unescaped quote ends a JSON string, or -1 while it is still being written.
func closingQuote(s string) int {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i
		}
	}
	return -1
}

// hexRune reads the four hex digits of a \u escape.
func hexRune(digits string) (rune, bool) {
	n, err := strconv.ParseUint(digits, 16, 16)
	return rune(n), err == nil
}

// unescape decodes JSON string escapes, dropping one cut off at the end of the text.
func unescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		if i+1 >= len(s) {
			break
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteString("    ")
		case 'r', 'b', 'f':
		case 'u':
			if i+4 >= len(s) {
				return b.String()
			}
			r, ok := hexRune(s[i+1 : i+5])
			i += 4
			if utf16.IsSurrogate(r) {
				// a character outside the basic plane is written as two escapes: an emoji is \ud83d\ude00
				if i+6 >= len(s) || s[i+1] != '\\' || s[i+2] != 'u' {
					continue // its second half has not arrived, or never will
				}
				low, _ := hexRune(s[i+3 : i+7])
				r, i = utf16.DecodeRune(r, low), i+6
			}
			if ok && utf8.ValidRune(r) {
				b.WriteRune(r)
			}
		default:
			b.WriteByte(s[i]) // \" \\ \/
		}
	}
	return b.String()
}
