package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecallHandsBackWhatTheConversationLost(t *testing.T) {
	store := NewResultStore(filepath.Join(t.TempDir(), "results"))
	if err := store.Save("r3", strings.Repeat("line\n", 4000)); err != nil {
		t.Fatal(err)
	}
	tool := &Recall{scope: scope{budget: 1000}, store: store}

	out, err := tool.Run(context.Background(), json.RawMessage(`{"id":"r3"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "saved result r3: ") {
		t.Fatalf("no header saying what came back:\n%s", cutUTF8(out, 120))
	}
	if !strings.Contains(out, "bytes omitted") {
		t.Fatalf("the tail was not cut with a note:\n%s", cutUTF8(out, 120))
	}
}

func TestRecallSaysWhenThereIsNothingToRecall(t *testing.T) {
	tool := &Recall{store: NewResultStore(filepath.Join(t.TempDir(), "results"))}
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"id":"r9"}`)); err == nil {
		t.Fatal("an unsaved id came back without an error")
	} else if !strings.Contains(err.Error(), "r9") {
		t.Errorf("err = %v, want it to name the id", err)
	}

	off := &Recall{store: NewResultStore("")}
	if _, err := off.Run(context.Background(), json.RawMessage(`{"id":"r1"}`)); err == nil {
		t.Fatal("a run that keeps nothing should say so")
	}
}

func TestRecallHandsTheSameResultOverOnlyOnce(t *testing.T) {
	store := NewResultStore(filepath.Join(t.TempDir(), "results"))
	if err := store.Save("r2", "the whole output"); err != nil {
		t.Fatal(err)
	}
	tool := NewRecall(store)

	args := json.RawMessage(`{"id":"r2"}`)
	first, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, "the whole output") {
		t.Fatalf("first recall is missing the text:\n%s", first)
	}
	again, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(again, "already handed over") {
		t.Fatalf("the second recall sent the text again (%d bytes):\n%s", len(again), again)
	}
}

func TestRecallPatternReturnsOnlyTheLinesItNeeds(t *testing.T) {
	store := NewResultStore(filepath.Join(t.TempDir(), "results"))
	var b strings.Builder
	for i := 1; i <= 60; i++ {
		if i == 20 || i == 45 {
			fmt.Fprintf(&b, "FAIL TestThing%d: something broke\n", i)
			continue
		}
		fmt.Fprintf(&b, "ok line %d of a long build log nobody wants to read again\n", i)
	}
	if err := store.Save("r7", b.String()); err != nil {
		t.Fatal(err)
	}
	tool := &Recall{scope: scope{budget: 4000}, store: store}

	out, err := tool.Run(context.Background(), json.RawMessage(`{"id":"r7","pattern":"^FAIL"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"FAIL TestThing20", "FAIL TestThing45", "ok line 19 of"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing from:\n%s", want, cutUTF8(out, 300))
		}
	}
	if strings.Contains(out, "ok line 30 of") {
		t.Errorf("lines far from a match came back too:\n%s", cutUTF8(out, 300))
	}
	if !strings.Contains(out, "lines, from offset 0") {
		t.Errorf("no header saying how much of the result this is:\n%s", cutUTF8(out, 200))
	}
}

func TestRecallOffsetAndLimitPageThroughAResult(t *testing.T) {
	store := NewResultStore(filepath.Join(t.TempDir(), "results"))
	var b strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	if err := store.Save("r8", b.String()); err != nil {
		t.Fatal(err)
	}
	tool := &Recall{scope: scope{budget: 4000}, store: store}

	out, err := tool.Run(context.Background(), json.RawMessage(`{"id":"r8","offset":20,"limit":10}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "10 of 100 lines, from offset 20") {
		t.Errorf("wrong header:\n%s", cutUTF8(out, 200))
	}
	if !strings.Contains(out, "line 21") || strings.Contains(out, "line 20\n") || strings.Contains(out, "line 31") {
		t.Errorf("the window is in the wrong place:\n%s", cutUTF8(out, 400))
	}
	if !strings.Contains(out, "offset 30") {
		t.Errorf("the cut does not say where to continue:\n%s", cutUTF8(out, 300))
	}
}

func TestASecondWholeRecallStillAnswersForAPart(t *testing.T) {
	store := NewResultStore(filepath.Join(t.TempDir(), "results"))
	if err := store.Save("r5", "first\nsecond\nthird\n"); err != nil {
		t.Fatal(err)
	}
	tool := &Recall{scope: scope{budget: 4000}, store: store}
	ctx := context.Background()

	if out, err := tool.Run(ctx, json.RawMessage(`{"id":"r5"}`)); err != nil || !strings.Contains(out, "first") {
		t.Fatalf("first recall: %q, %v", out, err)
	}
	part, err := tool.Run(ctx, json.RawMessage(`{"id":"r5","pattern":"third"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(part, "third") {
		t.Errorf("a filtered recall was refused after a full one:\n%s", part)
	}
	again, err := tool.Run(ctx, json.RawMessage(`{"id":"r5"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(again, "pattern or offset") {
		t.Errorf("the second whole recall did not point at the cheaper way:\n%s", again)
	}
}

func TestRecallComplainsAboutAPatternItCannotUse(t *testing.T) {
	store := NewResultStore(filepath.Join(t.TempDir(), "results"))
	if err := store.Save("r6", "one\ntwo\n"); err != nil {
		t.Fatal(err)
	}
	tool := &Recall{scope: scope{budget: 4000}, store: store}
	ctx := context.Background()

	if _, err := tool.Run(ctx, json.RawMessage(`{"id":"r6","pattern":"("}`)); err == nil ||
		!strings.Contains(err.Error(), "valid regex") {
		t.Errorf("a broken pattern: %v", err)
	}
	if _, err := tool.Run(ctx, json.RawMessage(`{"id":"r6","pattern":"zzz"}`)); err == nil ||
		!strings.Contains(err.Error(), "zzz") {
		t.Errorf("a pattern with no match: %v", err)
	}
}
