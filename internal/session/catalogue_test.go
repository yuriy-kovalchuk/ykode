package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// a listing says what a conversation was about, so one can be picked out again without its id
func TestASavedConversationDescribesItself(t *testing.T) {
	dir := t.TempDir()
	save(t, dir, "s20260101-120000", "the project", []llm.Message{
		{Role: "user", Content: "rename the helper\nand keep the tests green"},
		{Role: "assistant", Content: "done"},
	})
	save(t, dir, "s20260102-090000", "the project", []llm.Message{
		{Role: "user", Content: strings.Repeat("a long ask that goes on and on ", 8)},
	})

	list, err := Describe(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("%d conversations described, want 2", len(list))
	}
	if list[0].ID != "s20260102-090000" {
		t.Errorf("newest first broken: %s then %s", list[0].ID, list[1].ID)
	}
	if list[1].Messages != 2 || list[1].Root != "the project" {
		t.Errorf("the older conversation reads %d messages in %q", list[1].Messages, list[1].Root)
	}
	if list[1].First != "rename the helper and keep the tests green" {
		t.Errorf("first ask = %q", list[1].First)
	}
	if !strings.HasSuffix(list[0].First, "…") {
		t.Errorf("a long ask is not marked as cut: %q", list[0].First)
	}
	if list[0].Used.IsZero() {
		t.Error("the listing does not say when a conversation was last used")
	}
}

// a fold changes what a resume would read back, so it changes the listing too
func TestAFoldedConversationIsListedAsItNowStands(t *testing.T) {
	dir := t.TempDir()
	path := save(t, dir, "s20260101-120000", "the project", []llm.Message{
		{Role: "user", Content: "an ask that was folded away"},
		{Role: "assistant", Content: "and a long answer with it"},
	})
	log := New(dir, "s20260101-120000", "the project")
	if err := log.Folded(Fold{Kept: []llm.Message{{Role: "user", Content: "Goal: the fix\nNext step: tests"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}

	list, err := Describe(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Messages != 1 {
		t.Fatalf("the listing counts %d messages, want the one the fold left", list[0].Messages)
	}
	if list[0].First != "Goal: the fix Next step: tests" {
		t.Errorf("first ask = %q, want the recap the fold left", list[0].First)
	}
}

// nothing to resume means nothing in the listing: a broken file, and a conversation that was cleared
func TestAConversationThatCannotBeReadIsLeftOut(t *testing.T) {
	dir := t.TempDir()
	save(t, dir, "s20260101-120000", "the project", []llm.Message{{Role: "user", Content: "an ask"}})
	if err := os.WriteFile(filepath.Join(dir, "s20260100-000000.jsonl"), []byte("{ not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	log := New(dir, "s20260100-000100", "the project")
	if err := log.Append(llm.Message{Role: "user", Content: "an ask"}); err != nil {
		t.Fatal(err)
	}
	if err := log.Cleared(); err != nil {
		t.Fatal(err)
	}

	list, err := Describe(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != "s20260101-120000" {
		t.Errorf("the listing holds %v, want only the conversation there is something to resume", list)
	}
}

// the listing stops where it is told to
func TestTheListingKeepsToTheNewest(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"s20260101-120000", "s20260102-120000", "s20260103-120000"} {
		save(t, dir, id, "the project", []llm.Message{{Role: "user", Content: "an ask"}})
	}
	list, err := Describe(dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != "s20260103-120000" || list[1].ID != "s20260102-120000" {
		t.Errorf("the listing holds %v", list)
	}
}

// save writes one conversation the way a run would, and names the file it wrote.
func save(t *testing.T, dir, id, root string, messages []llm.Message) string {
	t.Helper()
	log := New(dir, id, root)
	for _, m := range messages {
		if err := log.Append(m); err != nil {
			t.Fatal(err)
		}
	}
	return log.Path()
}
