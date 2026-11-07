package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func editToolFixture(t *testing.T, content string) (*EditFile, func() string) {
	t.Helper()
	root, dir := treeFixture(t, map[string]string{"target.txt": content})
	path := filepath.Join(dir, "target.txt")
	current := func() string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	return NewEditFile(root, dir), current
}

func (EditFile) TestSpot() {} // keeps the receiver type honest in vet's eyes

func TestEditAppliesSeveralEditsInOneCall(t *testing.T) {
	tool, current := editToolFixture(t, "one\ntwo\nthree\n")
	out, err := tool.Run(context.Background(), json.RawMessage(`{"path":"target.txt","edits":[
		{"old_text":"one","new_text":"ONE"},
		{"old_text":"three","new_text":"THREE\nextra"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := "ONE\ntwo\nTHREE\nextra\n"; current() != want {
		t.Errorf("file = %q, want %q", current(), want)
	}
	if !strings.Contains(out, "2 replacements") {
		t.Errorf("result = %q, want it to report two replacements", out)
	}
}

func TestEditIsAtomicWhenOneEditIsBad(t *testing.T) {
	tool, current := editToolFixture(t, "one\ntwo\n")
	_, err := tool.Run(context.Background(), json.RawMessage(`{"path":"target.txt","edits":[
		{"old_text":"one","new_text":"ONE"},
		{"old_text":"not in the file","new_text":"nope"}]}`))
	if err == nil || !strings.Contains(err.Error(), "edit 2") {
		t.Fatalf("error = %v, want it to name edit 2", err)
	}
	if current() != "one\ntwo\n" {
		t.Errorf("file changed after a failed batch: %q", current())
	}
}

func TestEditRejectsAmbiguousAndOverlappingEdits(t *testing.T) {
	tool, _ := editToolFixture(t, "dup\ndup\n")
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"path":"target.txt","edits":[{"old_text":"dup","new_text":"x"}]}`)); err == nil ||
		!strings.Contains(err.Error(), "occurs 2 times") {
		t.Fatalf("duplicate error = %v", err)
	}
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"path":"target.txt","edits":[
		{"old_text":"dup\nd","new_text":"a"},
		{"old_text":"p\ndup","new_text":"b"}]}`)); err == nil ||
		!strings.Contains(err.Error(), "overlap") {
		t.Fatalf("overlap error = %v", err)
	}
}

func TestEditAcceptsEveryArgumentShapeModelsSend(t *testing.T) {
	cases := map[string]string{
		"flat":         `{"path":"target.txt","old_string":"one","new_string":"ONE"}`,
		"single array": `{"path":"target.txt","edits":[{"old_text":"one","new_text":"ONE"}]}`,
		"bare object":  `{"path":"target.txt","edits":{"old_text":"one","new_text":"ONE"}}`,
		"json string":  `{"path":"target.txt","edits":"[{\"old_text\":\"one\",\"new_text\":\"ONE\"}]"}`,
		"old names":    `{"path":"target.txt","edits":[{"old_string":"one","new_string":"ONE"}]}`,
	}
	for name, args := range cases {
		tool, current := editToolFixture(t, "one\n")
		if _, err := tool.Run(context.Background(), json.RawMessage(args)); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if current() != "ONE\n" {
			t.Errorf("%s: file = %q, want ONE", name, current())
		}
	}
}

func TestEditKeepsCRLFFilesWhenGivenLFText(t *testing.T) {
	tool, current := editToolFixture(t, "one\r\ntwo\r\n")
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"path":"target.txt","edits":[{"old_text":"one\ntwo","new_text":"ONE\nTWO"}]}`)); err != nil {
		t.Fatal(err)
	}
	if got := current(); got != "ONE\r\nTWO\r\n" {
		t.Errorf("file = %q, want CRLF preserved", got)
	}
}

func TestEditIgnoresWhitespaceDriftAsAFallback(t *testing.T) {
	tool, current := editToolFixture(t, "func a() {\n\t\treturn 1\n}\n")
	out, err := tool.Run(context.Background(), json.RawMessage(`{"path":"target.txt","edits":[{"old_text":"func a() {\nreturn 1\n}","new_text":"func a() {\n\treturn 2\n}"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if want := "func a() {\n\treturn 2\n}\n"; current() != want {
		t.Errorf("file = %q, want %q", current(), want)
	}
	if !strings.Contains(out, "ignoring extra whitespace") {
		t.Errorf("result = %q, want it to say the match was flexible", out)
	}
}

func TestEditStillRejectsTextThatIsNotThere(t *testing.T) {
	tool, current := editToolFixture(t, "alpha\nbeta\n")
	_, err := tool.Run(context.Background(), json.RawMessage(`{"path":"target.txt","edits":[{"old_text":"gamma","new_text":"x"}]}`))
	if err == nil || !strings.Contains(err.Error(), "does not occur") {
		t.Fatalf("error = %v", err)
	}
	if current() != "alpha\nbeta\n" {
		t.Errorf("file changed: %q", current())
	}
}

func TestEditRejectsIdenticalText(t *testing.T) {
	tool, _ := editToolFixture(t, "same\n")
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"path":"target.txt","edits":[{"old_text":"same","new_text":"same"}]}`)); err == nil ||
		!strings.Contains(err.Error(), "identical text") {
		t.Fatalf("error = %v", err)
	}
}

func TestEditOutsideRootStillNeedsApproval(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Dir(dir)
	if err := os.WriteFile(filepath.Join(parent, "outside.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	tool := NewEditFile(root, dir)

	if _, err := tool.Run(context.Background(), json.RawMessage(`{"path":"../outside.txt","edits":[{"old_text":"one","new_text":"two"}]}`)); err == nil ||
		!strings.Contains(err.Error(), "was not approved") {
		t.Fatalf("error = %v, want the approval refusal", err)
	}
	ctx := WithApprovals(context.Background(), []string{filepath.Join(parent, "outside.txt")})
	if _, err := tool.Run(ctx, json.RawMessage(`{"path":"../outside.txt","edits":[{"old_text":"one","new_text":"two"}]}`)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(parent, "outside.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%q", data) != `"two\n"` {
		t.Errorf("outside file = %q, want two", data)
	}
}

func TestAFailedEditDoesNotReportAnEarlierEdit(t *testing.T) {
	tool, _ := editToolFixture(t, "one\ntwo\nthree\n")
	ctx := context.Background()

	runTool(t, tool, `{"path":"target.txt","edits":[{"old_text":"two","new_text":"TWO"}]}`)
	if tool.LastDetail().Diff == "" {
		t.Fatal("the edit that ran left no diff for the audit log")
	}
	if _, err := tool.Run(ctx, json.RawMessage(`{"path":"target.txt","edits":[{"old_text":"absent","new_text":"x"}]}`)); err == nil {
		t.Fatal("an edit that matches nothing should fail")
	}
	if tool.LastDetail().Diff != "" {
		t.Errorf("the failed call still reports an earlier edit's diff:\n%s", tool.LastDetail().Diff)
	}
}

// a pair with no new_text is a slip, not a request to delete: it fails and leaves the file alone,
// while an explicit "" still deletes
func TestEditRefusesAPairWithoutNewText(t *testing.T) {
	for _, args := range []string{
		`{"path":"target.txt","edits":[{"old_text":"two\n"}]}`,
		`{"path":"target.txt","old_string":"two\n"}`,
	} {
		tool, current := editToolFixture(t, "one\ntwo\nthree\n")
		if _, err := tool.Run(context.Background(), json.RawMessage(args)); err == nil || !strings.Contains(err.Error(), "no new_text") {
			t.Errorf("%s: err = %v, want the missing new_text named", args, err)
		}
		if current() != "one\ntwo\nthree\n" {
			t.Errorf("%s: the file changed: %q", args, current())
		}
	}
	for _, args := range []string{
		`{"path":"target.txt","edits":[{"old_text":"two\n","new_text":""}]}`,
		`{"path":"target.txt","old_string":"two\n","new_string":""}`,
	} {
		tool, current := editToolFixture(t, "one\ntwo\nthree\n")
		if _, err := tool.Run(context.Background(), json.RawMessage(args)); err != nil {
			t.Fatalf("%s: %v", args, err)
		}
		if current() != "one\nthree\n" {
			t.Errorf("%s: an explicit empty new_text did not delete: %q", args, current())
		}
	}
}

// models send one change as two plain strings, though the schema only offers edits
func TestEditTakesOneChangeAsTopLevelText(t *testing.T) {
	tool, current := editToolFixture(t, "a\n\treturn x\n")
	args := map[string]string{"path": "target.txt", "old_text": "\treturn x", "new_text": "\treturn u.Name + \" <\" + u.Email + \">\""}
	raw, _ := json.Marshal(args)
	if _, err := tool.Run(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	if want := "a\n\treturn u.Name + \" <\" + u.Email + \">\"\n"; current() != want {
		t.Errorf("file = %q, want %q", current(), want)
	}
}

// both shapes in one call is ambiguous: which change was meant is not ours to guess
func TestEditRefusesBothShapesAtOnce(t *testing.T) {
	tool, current := editToolFixture(t, "one\ntwo\n")
	_, err := tool.Run(context.Background(), json.RawMessage(`{"path":"target.txt","old_text":"one","new_text":"ONE","edits":[{"old_text":"two","new_text":"TWO"}]}`))
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("err = %v, want both shapes refused", err)
	}
	if current() != "one\ntwo\n" {
		t.Errorf("the file changed: %q", current())
	}
}
