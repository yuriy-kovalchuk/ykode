package agent

import "testing"

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
