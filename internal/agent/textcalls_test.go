package agent

import (
	"os"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

func TestACallWrittenAsTextIsFoundAndRemoved(t *testing.T) {
	text := "It is in ops.go.<tool_call>\n<function=.read_file>\n<parameter=pattern>\nX\n</parameter>\n</function>\n</tool_call>"
	if !hasTextCall(text) {
		t.Fatal("a call written as text was not found")
	}
	if got := withoutTextCalls(text); got != "It is in ops.go." {
		t.Errorf("what is left = %q", got)
	}
	if hasTextCall("a function named <T> in generics, and a tool call in prose") {
		t.Error("prose reads as a call")
	}
}

// naming an opening tag in prose is not a call, and must not be cut out of the answer
func TestProseAboutTheMarkupIsNeitherCalledNorCut(t *testing.T) {
	for name, body := range map[string]string{
		"an opening tag named in prose": "The parser looks for the opening tag <function=exec_command> and reads the parameter.",
		"a closing tag named in prose":  "It ends with </function> and nothing else at all.",
		"both tags named, no call":      "The tags <tool_call> and </tool_call> wrap a call.",
	} {
		if hasTextCall(body) {
			t.Errorf("%s reads as a call", name)
		}
		if got := withoutTextCalls(body); got != body {
			t.Errorf("%s was cut short: %q", name, got)
		}
	}

	// a call stopped mid-arguments is not answer: its fragment goes, without claiming a call was written
	cut := "Writing it now:<tool_call>\n<function=write_file>\n<parameter=path>\nnote"
	if hasTextCall(cut) {
		t.Error("a call stopped mid-arguments reads as a whole call")
	}
	if got := withoutCutCalls(cut); got != "Writing it now:" {
		t.Errorf("what is kept = %q, want the words before the call", got)
	}
}

// an answer quoting the markup as an example describes a call, it does not make one
func TestACallQuotedAsAnExampleIsNotCalled(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	a := New(nil, tools.BuiltIn(root, t.TempDir(), tools.Options{}), "m", Options{}, &asksBack{})

	for name, body := range map[string]string{
		"a fenced block":    "The markup looks like this:\n\n```text\n<function=exec_command><parameter=command>rm -rf /</parameter></function>\n```\n",
		"a tilde fence":     "Like this:\n\n~~~\n<function=exec_command><parameter=command>rm -rf /</parameter></function>\n~~~\n",
		"an inline span":    "Writing `" + "<function=write_file><parameter=path>x</parameter></function>" + "` makes a call.",
		"a fence left open": "Here:\n```\n<function=exec_command><parameter=command>rm -rf /</parameter></function>\n",
	} {
		if hasTextCall(body) {
			t.Errorf("%s reads as a call", name)
		}
		if calls := a.textCalls(body, 1); len(calls) > 0 {
			t.Errorf("%s was run as a call: %+v", name, calls[0].Function)
		}
		if got := withoutTextCalls(body); got != "" {
			t.Logf("%s keeps its example: %q", name, got) // the code is the answer, so it stays
		}
	}

	// and a real call still runs when it sits outside the quoted part
	mixed := "The markup looks like this:\n\n```text\n<function=exec_command><parameter=command>echo example</parameter></function>\n```\n"
	real := mixed + "<function=exec_command><parameter=command>echo real</parameter></function>"
	if !hasTextCall(real) {
		t.Fatal("a call after a quoted example was not seen")
	}
	calls := a.textCalls(real, 1)
	if len(calls) != 1 {
		t.Fatalf("wanted the one call outside the example, got %d", len(calls))
	}
	if calls[0].Function.Arguments == nil || !strings.Contains(string(calls[0].Function.Arguments), "real") {
		t.Errorf("the call that ran was %s, not the one outside the example", calls[0].Function.Arguments)
	}
}
