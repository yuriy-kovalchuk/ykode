package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	draftPreviewLines = 8    // the newest lines of a call being written that the screen shows
	draftTailBytes    = 4096 // decode only this much of the end: a 100 KB file must not slow every token
)

// draftBody names the argument each tool's preview shows; other tools show their raw arguments.
var draftBody = map[string]string{"write_file": "content", "edit_file": "new_text", "exec_command": "command"}

// draft is a tool call the model is still writing.
type draft struct {
	call int
	tool string
	args string
}

// target is the path the call names so far; it comes first in the arguments, so it is known early.
func (d draft) target() string {
	value, _ := fieldValue(d.args, "path", false)
	return value
}

// preview is the newest lines of what the call writes, and how many lines and bytes it holds so far.
func (d draft) preview() (lines []string, total int, size int) {
	key, known := draftBody[d.tool]
	if !known {
		return tail(d.args), lineCount(d.args, "\n"), len(d.args)
	}
	value, raw := fieldValue(d.args, key, true)
	if raw == "" {
		return nil, 0, 0
	}
	return tail(value), lineCount(raw, `\n`), len(raw)
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
func draftHead(target string, lines, size int) string {
	parts := []string{}
	if target != "" {
		parts = append(parts, target)
	}
	if size > 0 {
		parts = append(parts, byteSize(size), fmt.Sprintf("%d lines", lines))
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

// fieldValue reads a string field out of JSON that may stop anywhere; tailOnly decodes just the end.
func fieldValue(args, key string, tailOnly bool) (value, raw string) {
	at := strings.LastIndex(args, `"`+key+`"`)
	if at < 0 {
		return "", ""
	}
	rest := strings.TrimLeft(args[at+len(key)+2:], " \t\n\r")
	if !strings.HasPrefix(rest, ":") {
		return "", ""
	}
	rest = strings.TrimLeft(rest[1:], " \t\n\r")
	if !strings.HasPrefix(rest, `"`) {
		return "", ""
	}
	raw = rest[1:]
	if end := closingQuote(raw); end >= 0 {
		raw = raw[:end]
	}
	from := raw
	if tailOnly && len(raw) > draftTailBytes {
		from = raw[len(raw)-draftTailBytes:]
		// start after an escaped newline, so decoding never begins inside an escape
		if nl := strings.Index(from, `\n`); nl >= 0 {
			from = from[nl+2:]
		}
	}
	return unescape(from), raw
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
			var r rune
			if _, err := fmt.Sscanf(s[i+1:i+5], "%04x", &r); err == nil && utf8.ValidRune(r) {
				b.WriteRune(r)
			}
			i += 4
		default:
			b.WriteByte(s[i]) // \" \\ \/
		}
	}
	return b.String()
}

// byteSize reads like a file listing: 640 B, 2.1 KB.
func byteSize(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f KB", float64(n)/1024)
}
