package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestACallIsReadableBack(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "audit")
	logger := New(dir, "s20260101-120000")
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	if err := logger.Append(Entry{Time: start, ID: "r1", Turn: 1, Root: "/p", Model: "m", Tool: "read_file", Args: `{"path":"a.go"}`, Status: "started"}); err != nil {
		t.Fatal(err)
	}
	done := Entry{Time: start.Add(time.Second), ID: "r1", Turn: 1, Root: "/p", Model: "m", Tool: "read_file", Args: `{"path":"a.go"}`, Status: "ok", ResultBytes: 12, DurationMS: 1000}
	if err := logger.Append(done); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, "2026-01.jsonl")); err != nil {
		t.Fatalf("no monthly log was written: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "2026-01.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("log mode is %o, want 0600", info.Mode().Perm())
	}
	entries, err := logger.Read("s20260101-120000")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Status != "started" || entries[1].Status != "ok" {
		t.Fatalf("read back %d entries: %+v", len(entries), entries)
	}
	if entries[0].Session != "s20260101-120000" {
		t.Errorf("the session was not stamped on the entry: %+v", entries[0])
	}
}

func TestAnotherSessionsLogIsNotMixedIn(t *testing.T) {
	logger := New(filepath.Join(t.TempDir(), "audit"), "mine")
	if err := logger.Append(Entry{ID: "r1", Tool: "read_file", Status: "ok"}); err != nil {
		t.Fatal(err)
	}
	other, err := logger.Read("someone-else")
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Errorf("reading another session returned %d entries", len(other))
	}
}

func TestASecretNeverReachesTheLog(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "audit")
	logger := New(dir, "s1")
	args := `{"command":"export API_KEY=hunter2 && go test","path":"x"}`
	if err := logger.Append(Entry{ID: "r1", Tool: "exec_command", Args: args, Status: "ok", Diff: "+password: swordfish", Paths: []string{"token=abc"}}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, time.Now().Format("2006-01")+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"hunter2", "swordfish", "abc"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("%q reached the log:\n%s", leak, raw)
		}
	}
	entries, err := logger.Read("s1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(entries[0].Args, "[masked]") {
		t.Errorf("the args were not masked: %s", entries[0].Args)
	}
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("the line is not valid JSON: %v", err)
	}
}

func TestALoggerWithNoDirectoryIsOff(t *testing.T) {
	logger := New("", "s1")
	if err := logger.Append(Entry{ID: "r1", Tool: "read_file", Status: "ok"}); err != nil {
		t.Errorf("a disabled logger failed: %v", err)
	}
	entries, err := logger.Read("s1")
	if err != nil || entries != nil {
		t.Errorf("a disabled logger read back %v, %v", entries, err)
	}
}
