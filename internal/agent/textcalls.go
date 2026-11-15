package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// Some models write a tool call into their answer instead of making it: the chat template taught them
// a markup, and the server did not read it back out. The call is there, whole, in words. Asking the
// model to make it "properly" works once and then the run ends on the second one, with the user left
// to type "continue"; so a call that names a tool we have, with arguments that close, is run as the
// call it is.
var (
	textFunctionRe  = regexp.MustCompile(`(?s)<function=([^>\s]+)>(.*?)</function>`)
	textParameterRe = regexp.MustCompile(`(?s)<parameter=([^>\s]+)>(.*?)</parameter>`)
	textJSONCallRe  = regexp.MustCompile(`(?s)<tool_call>\s*(\{.*?\})\s*</tool_call>`)
)

// textCallRe finds a tool call written out as text, the way some chat templates make models write it
// when they were not offered the tool interface or lost track of it.
var textCallRe = regexp.MustCompile(`(?s)<tool_call>.*?(?:</tool_call>|$)|<function=[^>]*>.*?(?:</function>|$)`)

func hasTextCall(s string) bool { return textCallRe.MatchString(outsideCode(s)) }

// codeSpanRe is what an answer sets off as code: a ``` fence, a ~~~ fence, or a ` backtick span. A
// fence left open reaches the end of the answer, which is the safer reading of an unfinished block.
var codeSpanRe = regexp.MustCompile("(?s)```.*?(```|$)|~~~.*?(~~~|$)|`[^`]*`")

const backtick = '`' // spelled once: a backtick inside a Go string literal is easy to misread

// outsideCode leaves out what the answer set off as code. Markup written as an example is being
// described, not used: running it would act on text whose whole purpose is to show what acting looks
// like, which is exactly what a document about this agent does when it names a call and its arguments.
func outsideCode(s string) string {
	if !strings.ContainsRune(s, backtick) && !strings.Contains(s, "~~~") {
		return s // nothing is set off: the common case costs one scan
	}
	return codeSpanRe.ReplaceAllString(s, " ")
}

// withoutTextCalls removes the calls written as text, leaving what the model said around them. What
// sits inside a code span is kept as written: a quoted example is the answer, not a call that failed.
func withoutTextCalls(s string) string {
	if !strings.ContainsRune(s, backtick) && !strings.Contains(s, "~~~") {
		return strings.TrimSpace(textCallRe.ReplaceAllString(s, ""))
	}
	var out strings.Builder
	at := 0
	for _, span := range codeSpanRe.FindAllStringIndex(s, -1) {
		out.WriteString(textCallRe.ReplaceAllString(s[at:span[0]], ""))
		out.WriteString(s[span[0]:span[1]]) // the code itself, exactly as it was written
		at = span[1]
	}
	out.WriteString(textCallRe.ReplaceAllString(s[at:], ""))
	return strings.TrimSpace(out.String())
}

// textCalls reads the calls a model wrote as text. It answers nil unless every one of them names a
// tool this run has: half a batch run and half ignored is worse than asking the model again.
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

// textValue is one argument written as text, as the JSON the tool expects. The markup has no types:
// a value is text between two tags, set off by a line break either side. An argument the tool's
// schema calls a string is that text; any other is read as the JSON it spells (5, true, [..]) when
// it spells some, and kept as text when it does not.
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

// The markup of a call written as text is not part of the answer, and it arrives a token at a time
// like the rest of it: callHider passes the answer through and holds back what lies between a call's
// opening tag and its closing one, so the screen never draws markup the conversation will not keep.
// A fenced block is passed through whole, because outsideCode keeps it in the answer too: what the
// reader watched arrive and what the conversation goes on to hold are the same text.
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
