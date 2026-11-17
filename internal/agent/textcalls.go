package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// some models write a tool call into their answer instead of making one, so a parseable one is run
var (
	textFunctionRe  = regexp.MustCompile(`(?s)<function=([^>\s]+)>(.*?)</function>`)
	textParameterRe = regexp.MustCompile(`(?s)<parameter=([^>\s]+)>(.*?)</parameter>`)
	textJSONCallRe  = regexp.MustCompile(`(?s)<tool_call>\s*(\{.*?\})\s*</tool_call>`)
)

// textCallCutRe also matches an unclosed tag: a half-call is not answer, and is not reported as one
var textCallCutRe = regexp.MustCompile(`(?s)<tool_call>.*?(?:</tool_call>|$)|<function=[^>]*>.*?(?:</function>|$)`)

// emptyCallTagsRe is the pair left once its call is cut out; a tag named in prose survives it
var emptyCallTagsRe = regexp.MustCompile(`(?s)<tool_call>\s*</tool_call>`)

// hasTextCall asks whether the text holds a call that parses, not whether any of the markup appears
func hasTextCall(s string) bool {
	s = outsideCode(s)
	return textFunctionRe.MatchString(s) || textJSONCallRe.MatchString(s)
}

// codeSpanRe is what an answer sets off as code; an open fence reaches the end of the answer
var codeSpanRe = regexp.MustCompile("(?s)```.*?(```|$)|~~~.*?(~~~|$)|`[^`]*`")

const backtick = '`' // spelled once: a backtick inside a Go string literal is easy to misread

// outsideCode leaves out what the answer set off as code: markup as an example is being described
func outsideCode(s string) string {
	if !strings.ContainsRune(s, backtick) && !strings.Contains(s, "~~~") {
		return s // nothing is set off: the common case costs one scan
	}
	return codeSpanRe.ReplaceAllString(s, " ")
}

// withoutTextCalls removes the calls written as text, keeping code spans and prose as written
func withoutTextCalls(s string) string {
	return cutCallMarkup(s, textFunctionRe, textJSONCallRe)
}

// withoutCutCalls drops the half of a call a reply out of tokens managed to write, and all after it
func withoutCutCalls(s string) string {
	return cutCallMarkup(s, textCallCutRe)
}

func cutCallMarkup(s string, res ...*regexp.Regexp) string {
	cut := func(part string) string {
		for _, re := range res {
			part = re.ReplaceAllString(part, "")
		}
		return emptyCallTagsRe.ReplaceAllString(part, "")
	}
	if !strings.ContainsRune(s, backtick) && !strings.Contains(s, "~~~") {
		return strings.TrimSpace(cut(s))
	}
	var out strings.Builder
	at := 0
	for _, span := range codeSpanRe.FindAllStringIndex(s, -1) {
		out.WriteString(cut(s[at:span[0]]))
		out.WriteString(s[span[0]:span[1]]) // the code itself, exactly as it was written
		at = span[1]
	}
	out.WriteString(cut(s[at:]))
	return strings.TrimSpace(out.String())
}

// textCalls answers nil unless every text call names a tool this run has: half a batch is worse
func (a *Agent) textCalls(content string, turn int) []llm.ToolCall {
	if a.tools == nil {
		return nil
	}
	content = outsideCode(content) // a call quoted as an example is not a call
	var calls []llm.ToolCall
	add := func(name string, args json.RawMessage) bool {
		if _, ok := a.tools.Get(name); !ok || !json.Valid(args) {
			return false
		}
		calls = append(calls, llm.ToolCall{
			ID:       fmt.Sprintf("text_%d_%d", turn, len(calls)+1),
			Type:     "function",
			Function: llm.FunctionCall{Name: name, Arguments: args},
		})
		return true
	}
	// <function=name><parameter=key>value</parameter></function>
	for _, m := range textFunctionRe.FindAllStringSubmatch(content, -1) {
		args := map[string]json.RawMessage{}
		for _, p := range textParameterRe.FindAllStringSubmatch(m[2], -1) {
			args[p[1]] = a.textValue(m[1], p[1], p[2])
		}
		raw, err := json.Marshal(args)
		if err != nil || !add(m[1], raw) {
			return nil
		}
	}
	if len(calls) > 0 {
		return calls
	}
	// <tool_call>{"name": ..., "arguments": {...}}</tool_call>
	for _, m := range textJSONCallRe.FindAllStringSubmatch(content, -1) {
		var call struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal([]byte(m[1]), &call) != nil {
			return nil
		}
		if len(call.Arguments) == 0 {
			call.Arguments = json.RawMessage("{}")
		}
		if !add(call.Name, call.Arguments) {
			return nil
		}
	}
	return calls
}

// textValue turns one text argument into the JSON the tool expects, since the markup has no types
func (a *Agent) textValue(tool, key, value string) json.RawMessage {
	value = strings.TrimSuffix(strings.TrimPrefix(value, "\n"), "\n")
	if a.schemaType(tool, key) != "string" {
		if spelled := strings.TrimSpace(value); spelled != "" && json.Valid([]byte(spelled)) {
			return json.RawMessage(spelled)
		}
	}
	raw, _ := json.Marshal(value)
	return raw
}

// schemaType is what a tool's schema says one of its arguments is, "string" when it does not say.
func (a *Agent) schemaType(tool, key string) string {
	t, ok := a.tools.Get(tool)
	if !ok {
		return "string"
	}
	var schema struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if json.Unmarshal(t.Schema(), &schema) != nil || schema.Properties[key].Type == "" {
		return "string"
	}
	return schema.Properties[key].Type
}

// callHider holds back a call's markup while it streams, so the screen never draws what is dropped
type callHider struct {
	held    string // text not passed on yet: it may be the start of a tag
	closing string // the tag that ends the call being hidden; "" while nothing is hidden
	fence   string // the fence we stand inside ("```" or "~~~"); "" in ordinary text
}

var callOpenings = map[string]string{"<tool_call>": "</tool_call>", "<function=": "</function>"}

var fenceOpenings = []string{"```", "~~~"}

// feed takes the next piece of the answer and returns what of it can be shown now.
func (h *callHider) feed(piece string) string {
	h.held += piece
	var shown strings.Builder
	for {
		if h.fence != "" { // inside a fence: everything is answer, markup included
			if end := strings.Index(h.held, h.fence); end >= 0 {
				shown.WriteString(h.held[:end+len(h.fence)])
				h.held, h.fence = h.held[end+len(h.fence):], ""
				continue
			}
			// no fence closes here: all of it is answer except a tail that may open the fence again
			keep := 0
			for n := min(len(h.fence)-1, len(h.held)); n > keep; n-- {
				if strings.HasSuffix(h.held, h.fence[:n]) {
					keep = n
					break
				}
			}
			shown.WriteString(h.held[:len(h.held)-keep])
			h.held = h.held[len(h.held)-keep:]
			return shown.String()
		}
		if h.closing != "" {
			end := strings.Index(h.held, h.closing)
			if end < 0 {
				h.held = h.held[max(len(h.held)-len(h.closing)+1, 0):] // only the start of the closing tag can matter
				return shown.String()
			}
			h.held, h.closing = h.held[end+len(h.closing):], ""
			continue
		}
		at, closing := -1, ""
		for open, shut := range callOpenings {
			if i := strings.Index(h.held, open); i >= 0 && (at < 0 || i < at) {
				at, closing = i, shut
			}
		}
		for _, open := range fenceOpenings {
			if i := strings.Index(h.held, open); i >= 0 && (at < 0 || i < at) {
				at, closing = i, "" // a fence opening: shown as written, and everything after it
			}
		}
		if at >= 0 {
			shown.WriteString(h.held[:at])
			if closing == "" {
				h.held, h.fence = h.held[at:], h.held[at:at+3]
				continue
			}
			h.held, h.closing = h.held[at:], closing
			continue
		}
		// nothing opens here: all of it is answer, except an ending that could be a tag's beginning
		keep := 0
		for open := range callOpenings {
			for n := min(len(open)-1, len(h.held)); n > keep; n-- {
				if strings.HasSuffix(h.held, open[:n]) {
					keep = n
					break
				}
			}
		}
		for _, open := range fenceOpenings {
			for n := min(len(open)-1, len(h.held)); n > keep; n-- {
				if strings.HasSuffix(h.held, open[:n]) {
					keep = n
					break
				}
			}
		}
		shown.WriteString(h.held[:len(h.held)-keep])
		h.held = h.held[len(h.held)-keep:]
		return shown.String()
	}
}

// rest is what was held back when the reply ended: the beginning of a tag that never became one.
func (h *callHider) rest() string {
	if h.closing != "" {
		return "" // a call that never closed: none of it was answer
	}
	held := h.held
	h.held = ""
	return held
}
