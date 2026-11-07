package tools

import (
	"context"
	"encoding/json"
	"testing"
)

func TestAWriteWithNoChangeDoesNotReportAnEarlierWrite(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{"note.txt": "one\n"})
	tool := NewWriteFile(root, dir)
	ctx := context.Background()

	write := func(content string) {
		t.Helper()
		args, err := json.Marshal(map[string]string{"path": "note.txt", "content": content})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tool.Run(ctx, args); err != nil {
			t.Fatal(err)
		}
	}

	write("two\n")
	if tool.LastDetail().Diff == "" {
		t.Fatal("a write that changed the file left no diff for the audit log")
	}
	write("two\n") // same text: nothing changed, so nothing is worth logging
	if tool.LastDetail().Diff != "" {
		t.Errorf("the unchanged write reports an earlier write's diff:\n%s", tool.LastDetail().Diff)
	}
}
