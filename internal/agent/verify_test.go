package agent

import "testing"

func TestTheNudgeRespectsARequestNotToUseTools(t *testing.T) {
	for _, p := range []string{"what was the error called? Do not use tools.", "answer without tools", "don't use any tools here"} {
		if !forbidsTools(p) {
			t.Errorf("%q does not read as a request for no tools", p)
		}
	}
	for _, p := range []string{"fix the bug in the tools package", "list the tools you have", "use tools to find it"} {
		if forbidsTools(p) {
			t.Errorf("%q reads as a request for no tools", p)
		}
	}
}

func TestACallWrittenAsTextIsFoundAndRemoved(t *testing.T) {
	text := "It is in ops.go.<tool_call>\n<function=.search_text>\n<parameter=pattern>\nX\n</parameter>\n</function>\n</tool_call>"
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
