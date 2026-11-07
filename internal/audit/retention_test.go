package audit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOldMonthsAgeOutAndTheCurrentOneNeverDoes(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"2000-01.jsonl", "2000-02.jsonl", time.Now().Format("2006-01") + ".jsonl", "notes.jsonl"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(`{"id":"r1"}`+"\n"), filePerm); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := PruneOldFiles(dir, 180*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 {
		t.Errorf("removed %d files, want the two old months", removed)
	}
	for _, name := range []string{time.Now().Format("2006-01") + ".jsonl", "notes.jsonl"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s should still be there: %v", name, err)
		}
	}
}
