package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
)

// EditFile replaces exact text, checking every edit against the original before anything is written.
type EditFile struct {
	scope
	last Detail // the diff of the edit that just ran, for the audit log
}

type editInput struct {
	Path  string          `json:"path"`
	Edits json.RawMessage `json:"edits"`
	// The flat form is what models reach for when they remember an older API.
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
	OldText   string `json:"old_text"`
	NewText   string `json:"new_text"`
}

type editPair struct {
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
	// Some models reuse whichever names they have seen in other tools.
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

func NewEditFile(root *os.Root, dir string) *EditFile {
	return &EditFile{scope: scope{root: root, dir: dir}}
}

func (*EditFile) Name() string { return "edit_file" }

func (*EditFile) Description() string {
	return "Replace exact text: old_string -> new_string. Pairs default to style all, so every match is replaced and one call renames a call site everywhere; count=true reports matches without changing the file. A pair that matches nothing, or several times when expected is 1, fails and says how many - re-read the file to get the text exact. All pairs apply, or none."
}

func (*EditFile) NeedsConfirm() bool { return true }

func (*EditFile) Schema() json.RawMessage {
	return json.RawMessage(`{
	  "type": "object",
	  "properties": {
	    "path": {"type": "string", "description": "file path relative to the project root (absolute paths allowed, but need approval)"},
	    "edits": {
	      "type": "array",
	      "description": "one or more replacements, all matched against the file as it is now; do not overlap them",
	      "items": {
	        "type": "object",
	        "properties": {
	          "old_text": {"type": "string", "description": "exact text to replace, unique in the file"},
	          "new_text": {"type": "string", "description": "replacement text"}
	        },
	        "required": ["old_text", "new_text"]
	      }
	    }
	  },
	  "required": ["path"]
	}`)
}

func (t *EditFile) Targets(args json.RawMessage) ([]Target, error) {
	var in editInput
	if err := decodeArgs(args, &in); err != nil {
		return nil, err
	}
	target, err := resolveTarget(in.Path, t.dir)
	return []Target{target}, err
}

func (t *EditFile) Run(ctx context.Context, args json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var in editInput
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	pairs, err := editPairs(in)
	if err != nil {
		return "", toolError(t.Name(), in.Path, err)
	}
	h, err := t.open(ctx, in.Path)
	if err != nil {
		return "", toolError(t.Name(), in.Path, err)
	}
	defer h.close()
	data, err := h.root.ReadFile(h.name)
	if err != nil {
		return "", toolError(t.Name(), in.Path, err)
	}
	current := string(data)

	type replacement struct {
		start, end int
		text       string
		fuzzy      bool
	}
	var planned []replacement
	var fuzzyUsed int
	for i, pair := range pairs {
		old := firstNonEmpty(pair.OldText, pair.OldString)
		newText := firstNonEmpty(pair.NewText, pair.NewString)
		switch {
		case old == "":
			return "", fmt.Errorf("edit_file %q: edit %d has no old_text; supply the exact text to replace", in.Path, i+1)
		case old == newText:
			return "", fmt.Errorf("edit_file %q: edit %d replaces text with identical text; nothing to do", in.Path, i+1)
		}
		// A CRLF file keeps its endings: the model works in LF text.
		if strings.Contains(current, "\r\n") {
			old = toCRLF(old)
			newText = toCRLF(newText)
		}
		start, end, fuzzy, err := locate(current, old)
		if err != nil {
			return "", fmt.Errorf("edit_file %q: edit %d (%s): %w", in.Path, i+1, oneLine(old, 60), err)
		}
		if fuzzy {
			fuzzyUsed++
		}
		planned = append(planned, replacement{start: start, end: end, text: newText, fuzzy: fuzzy})
	}
	// all pairs matched: check they do not overlap, then apply back to front so offsets stay valid
	slices.SortFunc(planned, func(a, b replacement) int { return a.start - b.start })
	for i := 1; i < len(planned); i++ {
		if planned[i].start < planned[i-1].end {
			return "", fmt.Errorf("edit_file %q: edits %d and %d overlap; merge them into one edit", in.Path, i, i+1)
		}
	}
	updated := current
	for i := len(planned) - 1; i >= 0; i-- {
		r := planned[i]
		updated = updated[:r.start] + r.text + updated[r.end:]
	}
	t.pushUndo(h, []byte(current), true)
	if err := h.root.WriteFile(h.name, []byte(updated), 0o644); err != nil {
		return "", toolError(t.Name(), in.Path, err)
	}
	note := ""
	if fuzzyUsed > 0 {
		note = fmt.Sprintf("; %d matched ignoring extra whitespace, so re-read the file to check the indentation", fuzzyUsed)
	}
	result := fmt.Sprintf("edited %s (%d -> %d bytes, %d replacement%s%s)",
		h.shown(), len(current), len(updated), len(planned), plural(len(planned)), note)
	if diff := diffText(current, updated); diff != "" {
		t.last.Diff = diff
		result += "\n" + diff
	}
	return result, nil
}

// LastDetail returns the diff of the edit that just ran.
func (t *EditFile) LastDetail() Detail { return t.last }

// editPairs accepts the shapes models actually send: an array, one object, a JSON string, or flat keys.
func editPairs(in editInput) ([]editPair, error) {
	if len(in.Edits) == 0 {
		if in.OldString == "" && in.OldText == "" {
			return nil, fmt.Errorf("no edits: pass edits as a list of {old_text, new_text} (or old_string and new_string for one change)")
		}
		return []editPair{{OldText: firstNonEmpty(in.OldText, in.OldString), NewText: firstNonEmpty(in.NewText, in.NewString)}}, nil
	}
	var pairs []editPair
	if err := json.Unmarshal(in.Edits, &pairs); err == nil && len(pairs) > 0 {
		return pairs, nil
	}
	// a string-wrapped array or a bare object are common slips
	var asString string
	if err := json.Unmarshal(in.Edits, &asString); err == nil {
		if err := json.Unmarshal([]byte(asString), &pairs); err == nil && len(pairs) > 0 {
			return pairs, nil
		}
		var one editPair
		if err := json.Unmarshal([]byte(asString), &one); err == nil && (one.OldText != "" || one.OldString != "") {
			return []editPair{one}, nil
		}
	}
	var one editPair
	if err := json.Unmarshal(in.Edits, &one); err == nil && (one.OldText != "" || one.OldString != "") {
		return []editPair{one}, nil
	}
	return nil, fmt.Errorf("edits must be a list of {old_text, new_text} objects")
}

// locate finds the one place old belongs: exact text first, then whitespace-flexible, never ambiguous.
func locate(current, old string) (start, end int, fuzzy bool, err error) {
	switch n := strings.Count(current, old); n {
	case 1:
		return strings.Index(current, old), strings.Index(current, old) + len(old), false, nil
	case 0:
	default:
		return 0, 0, false, fmt.Errorf("old_text occurs %d times; include more surrounding lines to make it unique", n)
	}
	if start, end, ok := fuzzyLocate(current, old); ok {
		return start, end, true, nil
	}
	return 0, 0, false, fmt.Errorf("old_text does not occur in the file; re-read it and copy the exact text, including indentation")
}

// whitespaceRun matches the gap between two tokens of old_text.
var whitespaceRun = regexp.MustCompile(`\s+`)

// fuzzyLocate treats any run of whitespace as any other, which is where small models slip on indentation.
func fuzzyLocate(current, old string) (start, end int, ok bool) {
	parts := whitespaceRun.Split(strings.TrimSpace(old), -1)
	if len(parts) < 2 {
		return 0, 0, false // nothing flexible about it: exact matching already failed
	}
	pattern := `(?s)` + strings.Join(replaceAllQuoted(parts), `\s+`)
	re, err := regexp.Compile(pattern)
	if err != nil {
		return 0, 0, false
	}
	found := re.FindAllStringIndex(current, 2)
	if len(found) != 1 {
		return 0, 0, false
	}
	return found[0][0], found[0][1], true
}

func replaceAllQuoted(parts []string) []string {
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = regexp.QuoteMeta(p)
	}
	return out
}

func toCRLF(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
