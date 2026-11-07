package tools

import (
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

func TestAStoreDirectoryIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "results")
	if err := os.MkdirAll(path, 0o755); err != nil { // as an older ykode made it
		t.Fatal(err)
	}
	store := NewResultStore(path)
	if err := store.Save("r1", "the raw output of a command nobody else should read"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("the store dir is %o, and it holds raw command output", info.Mode().Perm())
	}
}

func TestASavedResultIsNeverWrittenOver(t *testing.T) {
	store := NewResultStore(filepath.Join(t.TempDir(), "results"))
	if err := store.Save("r1", "what the first run saw"); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("r1", "what some other run saw"); err == nil {
		t.Error("a second result took the id of the first")
	}
	text, err := store.Load("r1")
	if err != nil {
		t.Fatal(err)
	}
	if text != "what the first run saw" {
		t.Errorf("recall r1 would now hand back: %q", text)
	}
}
