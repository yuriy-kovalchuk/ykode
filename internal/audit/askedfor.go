package audit

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// What a call was asked to do, as rows a person reads rather than as the JSON the model sent.
//
// The rules that make it readable:
//   - one row per argument, in the order the model wrote them, so a command comes before the flags that
//     change it and not in the order the alphabet happens to put them
//   - a value that fits one line is written out, not cut: the point of a command row is to read the
//     command, and a caller with little room wraps the row rather than cutting the thought
//   - a value spanning lines (a file's content, a search's results) keeps its first line and is counted,
//     because nobody reads four hundred rows in a list; the whole of it belongs to the view of one call,
//     which shows every line of it and of anything nested inside, however long, and scrolls
//   - a value that is several things is counted, and their words shown when they are short
//   - arguments that are not valid JSON are still worth reading: a log line written before the arguments
//     were kept whole holds a cut fragment, and its leading pairs are salvaged

const (
	askedForWords  = 4   // a value that is this few things shows their words rather than a count
	askedForSample = 18  // ...or this many characters of the first one, when that is shorter still
	listValueChars = 300 // in a list, one line this long is cut with the rest counted
)

type argument struct {
	name, value string

	// raw is the JSON of a value holding several things, kept so the view of one call can take it apart;
	// empty once the value has been joined onto its name
	raw                 string
	lines, bytes, items int
	nested              bool // too big to quote in a list: it is counted there
}

// AskedFor is the compact form, for a list and the pane under it.
func AskedFor(args string) []string {
	fields := argumentsIn(args)
	if len(fields) == 0 {
		return nil
	}
	rows := make([]string, 0, len(fields))
	for _, f := range fields {
		rows = append(rows, listRows(f)...)
	}
	return rows
}

// WholeArgs is the form for the view of one call: everything the model asked for, every line of a multi-line
// value hanging under its name, and the parts of a value that holds several things each read the same way.
func WholeArgs(args string) []string {
	var rows []string
	for _, f := range argumentsIn(args) {
		rows = append(rows, wholeRows(f, "")...)
	}
	return rows
}

// wholeRows is one argument read in full: its text whole, a block hanging under its name with its own
// indentation kept, and each part of a value holding several things read the same way under it.
func wholeRows(f argument, gap string) []string {
	if parts := partsOf(f); len(parts) > 0 {
		rows := []string{gap + f.name + ":"}
		for _, part := range parts {
			rows = append(rows, wholeRows(part, gap+"  ")...)
		}
		return rows
	}
	if f.lines <= 1 {
		return []string{gap + f.name + ": " + strings.Join(strings.Fields(f.value), " ")}
	}
	body := strings.Split(strings.TrimRight(f.value, "\n"), "\n")
	rows := []string{gap + f.name + ": " + body[0]}
	for _, line := range body[1:] {
		rows = append(rows, gap+"  "+line)
	}
	return rows
}

// partsOf takes a value that holds several things apart: the items of an array in their order, the fields of
// an object in the order they were written.
func partsOf(f argument) []argument {
	raw := strings.TrimSpace(f.raw)
	if raw == "" {
		return nil
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if json.Unmarshal([]byte(raw), &items) != nil {
			return nil
		}
		out := make([]argument, 0, len(items))
		for i, item := range items {
			out = append(out, fieldOf(fmt.Sprintf("%d", i+1), string(item)))
		}
		return out
	}
	return argumentsIn(raw)
}

// listRows is one argument as a list shows it: its text where there is room for it, its size where there is
// not, so a hundred search hits do not push the list off the screen.
func listRows(f argument) []string {
	switch {
	case f.nested:
		return []string{f.name + ": " + counted(f)}
	case f.lines <= 1:
		value := strings.Join(strings.Fields(f.value), " ")
		if len(value) > listValueChars {
			value = fmt.Sprintf("%s … %d characters more", value[:listValueChars], len(value)-listValueChars)
		}
		return []string{f.name + ": " + value}
	}
	body := strings.Split(strings.TrimRight(f.value, "\n"), "\n")
	return []string{fmt.Sprintf("%s: %s · %s · %s", f.name, strings.TrimSpace(body[0]), lines(len(body)), bytes(f.bytes))}
}

// lines says how many lines something is, in the one case where the plural reads wrong.
func lines(n int) string {
	if n == 1 {
		return "1 line"
	}
	return fmt.Sprintf("%d lines", n)
}

// counted says how big something too big to quote is, and samples it when that is still shorter.
func counted(f argument) string {
	tally := bytes(f.bytes)
	if f.items > 0 {
		tally = fmt.Sprintf("%d parts · %s", f.items, tally)
	}
	if f.lines > 1 {
		tally = lines(f.lines) + " · " + tally
	}
	if !f.nested && f.bytes <= askedForSample && f.lines <= 3 {
		return tally + " · " + strings.Join(strings.Fields(f.value), " ")
	}
	return tally
}

// bytes makes a size say itself in the units a person expects: a search that hit 600 KB is not "614400 B".
func bytes(n int) string {
	switch {
	case n >= 1<<20:
		return strconv.FormatFloat(float64(n)/float64(1<<20), 'f', 1, 64) + " MB"
	case n >= 1<<10:
		return strconv.FormatFloat(float64(n)/float64(1<<10), 'f', 1, 64) + " KB"
	}
	return strconv.Itoa(n) + " B"
}

// argumentsIn reads the call's JSON into ordered fields: a string, number or bool kept as text, an object or
// array described by size. Invalid JSON goes to salvage.
func argumentsIn(args string) []argument {
	args = strings.TrimSpace(args)
	if args == "" {
		return nil
	}
	if !json.Valid([]byte(args)) {
		return salvage(args)
	}
	dec := json.NewDecoder(strings.NewReader(args))
	if _, err := dec.Token(); err != nil { // the opening brace
		return nil
	}
	var out []argument
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			break
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			break
		}
		name := fmt.Sprintf("%v", key)
		if name == "" {
			continue
		}
		out = append(out, fieldOf(name, string(raw)))
	}
	return out
}

// fieldOf describes one argument's JSON value.
func fieldOf(name, raw string) argument {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return argument{name: name}
	}
	switch raw[0] {
	case '{', '[':
		f := argument{name: name, value: raw, raw: raw, nested: true, lines: strings.Count(raw, "\n") + 1, bytes: len(raw)}
		if raw[0] == '[' {
			var items []json.RawMessage
			if json.Unmarshal([]byte(raw), &items) == nil {
				f.items = len(items)
			}
		}
		if words := joined(raw); words != "" {
			f.value, f.raw, f.nested, f.lines = words, "", false, 1
			f.bytes = len(words)
		}
		return f
	case '"':
		var text string
		if json.Unmarshal([]byte(raw), &text) != nil {
			text = strings.Trim(raw, `"`)
		}
		return argument{name: name, value: text, lines: strings.Count(text, "\n") + 1, bytes: len(text)}
	default: // a number, a bool
		return argument{name: name, value: raw, lines: 1, bytes: len(raw)}
	}
}

// joined turns an array of words into one line, so a list of patterns reads as a list of patterns; an array
// of anything else is left to the count.
func joined(raw string) string {
	var words []string
	if json.Unmarshal([]byte(raw), &words) != nil {
		return ""
	}
	kept := 0
	for _, w := range words {
		if w != "" {
			kept++
		}
	}
	if kept == 0 {
		return ""
	}
	return strings.Join(words, " ")
}

// salvage reads the "name":"value" pairs that survive at the start of a fragment whose JSON was cut short,
// which is what a log line written before the arguments were kept whole holds. It stops at the first pair
// that cannot be read.
func salvage(args string) []argument {
	var out []argument
	body := strings.TrimPrefix(strings.TrimSpace(args), "{")
	for len(body) > 0 {
		key, rest, ok := cutQuoted(body, ",\n\t ")
		if !ok {
			break
		}
		rest = strings.TrimSpace(rest)
		if !strings.HasPrefix(rest, ":") {
			break
		}
		value, tail, closed := cutQuoted(strings.TrimSpace(rest[1:]), "\n\t ")
		f := argument{name: key, value: unescaped(value), bytes: len(value)}
		if !closed { // the fragment ended inside this value: say so, rather than let it look complete
			if tail := strings.TrimRight(f.value, " "); !strings.HasSuffix(tail, "…") && !strings.HasSuffix(tail, "...") {
				f.value += " …"
			}
		}
		f.lines = strings.Count(f.value, "\n") + 1
		out = append(out, f)
		if !closed {
			break
		}
		body = tail
	}
	return out
}

// cutQuoted takes a quoted run off the front of s, reporting whether the closing quote was found.
func cutQuoted(s, skip string) (string, string, bool) {
	s = strings.TrimLeft(s, skip)
	if !strings.HasPrefix(s, `"`) {
		return "", s, false
	}
	for i := 1; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == '"' {
			return s[1:i], s[i+1:], true
		}
	}
	return s[1:], "", false
}

// unescaped turns the escapes a JSON string holds back into text, falling back to the common ones when the
// fragment is too broken for a real decode.
func unescaped(raw string) string {
	if s, err := strconv.Unquote(`"` + raw + `"`); err == nil {
		return s
	}
	repl := strings.NewReplacer("\\n", "\n", "\\t", "\t", "\\\"", "\"", "\\\\", "\\")
	return repl.Replace(raw)
}
