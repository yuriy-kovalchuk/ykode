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

// an answer that quotes the markup as an example is describing a call, not making one. A document about
// this agent names a call and its arguments to explain them, and running that would act on text whose
// whole job is to show what acting looks like: the fence is how the answer says it is an example.
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
