package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestASavedResultComesBackWhole(t *testing.T) {
	store := NewResultStore(filepath.Join(t.TempDir(), "results"))
	if err := store.Save("r7", "the whole output"); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load("r7")
	if err != nil {
		t.Fatal(err)
	}
	if got != "the whole output" {
		t.Fatalf("got %q, want the text that went in", got)
	}
}

func TestAStoringRefusesAnIdThatIsNotAnId(t *testing.T) {
	dir := t.TempDir()
	storeDir := filepath.Join(dir, "results")
	store := NewResultStore(storeDir)
	// a file outside the store exists precisely so an unvalidated id could reach it
	if err := os.WriteFile(filepath.Join(dir, "outside.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "r", "rr7", "7", "../outside.txt", "../outside", "r7; rm -rf", "R7"} {
		err := store.Save(bad, "x")
		if err == nil {
			t.Errorf("Save(%q) was accepted", bad)
		}
		text, err := store.Load(bad)
		if err == nil {
			t.Errorf("Load(%q) was accepted and read %q", bad, text)
		} else if !strings.Contains(err.Error(), "ids look like") {
			t.Errorf("Load(%q) = %v, want it refused as a bad id", bad, err)
		}
	}
	entries, err := os.ReadDir(storeDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(entries) > 0 {
		t.Fatalf("a refused id still wrote into the store: %v", entries)
	}
}

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
