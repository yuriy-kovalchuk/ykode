package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runExec(t *testing.T, args string) string {
	t.Helper()
	out, err := NewExec(t.TempDir()).Run(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("exec_command %s: %v", args, err)
	}
	return out
}

func TestExecTimeoutComesFromTheArguments(t *testing.T) {
	start := time.Now()
	out := runExec(t, `{"command":"sleep 5","timeout":1}`)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("took %v; the timeout argument was not honoured", elapsed)
	}
	if !strings.Contains(out, "[command killed after its 1s timeout") {
		t.Fatalf("result = %q, want the timeout note", out)
	}
}

func TestExecTimeoutIsClamped(t *testing.T) {
	cases := []struct {
		seconds int
		want    time.Duration
		capped  bool
	}{
		{0, defaultExecTimeout, false},
		{-3, defaultExecTimeout, false},
		{5, 5 * time.Second, false},
		{300, 5 * time.Minute, false},
		{100000, maxExecTimeout, true},
	}
	for _, c := range cases {
		got, capped := execTimeout(c.seconds)
		if got != c.want || capped != c.capped {
			t.Errorf("execTimeout(%d) = (%v, %v), want (%v, %v)", c.seconds, got, capped, c.want, c.capped)
		}
	}
}

func TestExecReportsExitStatus(t *testing.T) {
	if out := runExec(t, `{"command":"echo boom; exit 3"}`); !strings.Contains(out, "boom") ||
		!strings.Contains(out, "[exit status 3]") {
		t.Fatalf("result = %q", out)
	}
}

func TestExecLongOutputSavesTheFullLog(t *testing.T) {
	// Keep the spill log inside the test's own tree instead of the shared temp
	// directory, which nothing cleans up until the next ykode start.
	t.Setenv("TMPDIR", t.TempDir())
	dir := t.TempDir()
	tool := NewExec(dir)
	out, err := tool.Run(context.Background(), json.RawMessage(`{"command":"seq 1 4000"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > maxResultBytes+512 {
		t.Fatalf("result is %d bytes, over the %d byte cap", len(out), maxResultBytes)
	}
	if !strings.Contains(out, "bytes omitted") {
		t.Fatalf("result = %q, want a note about omitted bytes", out)
	}
	path := savedLogPath(out)
	if path == "" {
		t.Fatalf("result = %q, want the path of the full output", out)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("saved log unreadable: %v", err)
	}
	if !strings.HasSuffix(string(data), "4000\n") {
		t.Fatalf("saved log is %d bytes, want the whole output", len(data))
	}
	// The tail is where build and test failures live, so it must survive the cut.
	if !strings.Contains(out, "3999") {
		t.Fatalf("result should keep the tail of the output:\n%s", cutUTF8(out, 200))
	}
}

// savedLogPath pulls the "full output: PATH" note out of a truncated result.
func savedLogPath(out string) string {
	const marker = "full output: "
	i := strings.Index(out, marker)
	if i < 0 {
		return ""
	}
	rest := out[i+len(marker):]
	return strings.Fields(rest)[0] // the path never contains spaces
}

func TestExecEmptyCommandIsRejected(t *testing.T) {
	if _, err := NewExec(t.TempDir()).Run(context.Background(), json.RawMessage(`{"command":"  "}`)); err == nil ||
		!strings.Contains(err.Error(), "command is empty") {
		t.Fatalf("error = %v", err)
	}
}

func TestExecCommandsDoNotInheritTheParentEnvironment(t *testing.T) {
	t.Setenv("YKODE_TEST_SECRET", "leaked-token")

	out := runExec(t, `{"command":"echo [$YKODE_TEST_SECRET]; echo PATH=$PATH; echo HOME=$HOME"}`)
	if strings.Contains(out, "leaked-token") {
		t.Fatalf("a token in ykode's own environment reached the model's shell:\n%s", cutUTF8(out, 300))
	}
	if !strings.Contains(out, "PATH=/") {
		t.Fatalf("PATH must survive the scrub, builds and git depend on it:\n%s", cutUTF8(out, 300))
	}
	if os.Getenv("HOME") != "" && !strings.Contains(out, "HOME=/") {
		t.Fatalf("HOME must survive the scrub:\n%s", cutUTF8(out, 300))
	}
}

func TestExecMinimalEnvStillRunsTheToolchain(t *testing.T) {
	out := runExec(t, `{"command":"go version && git --version"}`)
	if !strings.Contains(out, "go version") || !strings.Contains(out, "git version") {
		t.Fatalf("the scrubbed env broke an ordinary build tool call:\n%s", cutUTF8(out, 400))
	}
}

func TestPruneSpillLogsRemovesOnlyOldSpillDirs(t *testing.T) {
	dir := t.TempDir()
	fresh := filepath.Join(dir, spillPrefix+"fresh")
	stale := filepath.Join(dir, spillPrefix+"stale")
	other := filepath.Join(dir, "not-a-spill-dir")
	for _, d := range []string{fresh, stale, other} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "output.log"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	aged := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(stale, aged, aged); err != nil {
		t.Fatal(err)
	}

	removed, err := PruneSpillLogs(dir, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed %d dirs, want exactly the stale one", removed)
	}
	for _, d := range []string{fresh, other} {
		if _, err := os.Stat(d); err != nil {
			t.Fatalf("%s should have survived: %v", d, err)
		}
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("a day-old spill dir survived the prune")
	}
}

func TestPruneSpillLogsReportsAnUnreadableDirectory(t *testing.T) {
	if _, err := PruneSpillLogs(filepath.Join(t.TempDir(), "missing"), time.Hour); err == nil {
		t.Fatal("a missing directory must be reported, not swallowed")
	}
}
