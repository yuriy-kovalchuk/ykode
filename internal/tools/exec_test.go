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
		def     time.Duration
		want    time.Duration
		capped  bool
	}{
		{0, defaultExecTimeout, defaultExecTimeout, false},
		{-3, defaultExecTimeout, defaultExecTimeout, false},
		{0, 5 * time.Minute, 5 * time.Minute, false},
		{5, defaultExecTimeout, 5 * time.Second, false},
		{300, defaultExecTimeout, 5 * time.Minute, false},
		{100000, defaultExecTimeout, maxExecTimeout, true},
	}
	for _, c := range cases {
		got, capped := execTimeout(c.seconds, c.def)
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
	// keep the spill log in this test's own tree; nothing else cleans the shared temp dir
	t.Setenv("TMPDIR", t.TempDir())
	dir := t.TempDir()
	tool := NewExec(dir)
	out, err := tool.Run(context.Background(), json.RawMessage(`{"command":"seq 1 4000"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > defaultResultBytes+512 {
		t.Fatalf("result is %d bytes, over the %d byte cap", len(out), defaultResultBytes)
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
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permission this test relies on")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if _, err := PruneSpillLogs(dir, time.Hour); err == nil {
		t.Fatal("an unreadable log directory must be reported, not swallowed")
	}
}

// One folder the sweep cannot remove must not strand everything listed after it: the whole point of
// the sweep is that nothing too old is left behind, and a single stubborn folder would undo that at
// every start.
func TestPruneSpillLogsKeepsGoingPastOneItCannotRemove(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permission this test relies on")
	}
	dir := t.TempDir()
	old := time.Now().Add(-48 * time.Hour)

	// named so the stubborn one is listed first: the point is that the sweep continues past it.
	// written while the folder is still writable, then closed: a read-only folder holding a file
	// cannot be emptied, so removing it fails
	stubborn := filepath.Join(dir, spillPrefix+"a-stubborn")
	if err := os.Mkdir(stubborn, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stubborn, "inside"), []byte("x"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stubborn, 0o500); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(dir, spillPrefix+"b-going")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(stubborn, 0o700) })
	for _, each := range []string{stubborn, gone} {
		if err := os.Chtimes(each, old, old); err != nil {
			t.Fatal(err)
		}
	}

	removed, err := PruneSpillLogs(dir, 24*time.Hour)
	if err == nil {
		t.Error("a folder left behind was not reported")
	}
	if removed != 1 {
		t.Errorf("removed %d, want the one folder that could go removed anyway", removed)
	}
	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		t.Error("the removable folder was stranded by the one that was not")
	}
}

func TestCutCommandOutputLandsInTheConfiguredLogDir(t *testing.T) {
	logs := t.TempDir()
	tool := NewExec(t.TempDir())
	tool.LogDir = logs

	out := runTool(t, tool, `{"command":"seq 1 4000"}`)
	marker := "full output: "
	at := strings.Index(out, marker)
	if at < 0 {
		t.Fatalf("no log path in: %.200q", out)
	}
	path := strings.Fields(out[at+len(marker):])[0]
	if !strings.HasPrefix(path, filepath.Join(logs, spillPrefix)) {
		t.Fatalf("log path %q, want it under the configured log dir %q", path, logs)
	}
}

func TestPruneToleratesAFirstRunWithNoLogs(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "logs")
	removed, err := PruneSpillLogs(missing, time.Hour)
	if err != nil || removed != 0 {
		t.Fatalf("PruneSpillLogs() = (%d, %v), want no error for a directory that does not exist yet", removed, err)
	}
}

func TestEscapeSequencesNeverReachTheModel(t *testing.T) {
	tool := NewExec(t.TempDir())

	out := runTool(t, tool, `{"command":"printf '\\033[31mFAIL\\033[0m TestParse\\n'"}`)
	if strings.Contains(out, "\x1b") {
		t.Fatalf("raw escapes survived: %q", out)
	}
	if !strings.Contains(out, "FAIL TestParse") {
		t.Fatalf("text was lost with the escapes: %q", out)
	}
}

func TestAProgressBarKeepsItsLastFrame(t *testing.T) {
	tool := NewExec(t.TempDir())

	out := runTool(t, tool, `{"command":"printf 'downloading 10%%\\rdownloading 50%%\\rdownloading 100%%\\n'"}`)
	if strings.Contains(out, "downloading 10%") || strings.Contains(out, "downloading 50%") {
		t.Fatalf("earlier frames survived: %q", out)
	}
	if !strings.Contains(out, "downloading 100%") {
		t.Fatalf("last frame missing: %q", out)
	}
}

func TestARunOfIdenticalLinesIsCollapsed(t *testing.T) {
	tool := NewExec(t.TempDir())

	out := runTool(t, tool, `{"command":"for i in 1 2 3 4 5 6 7 8 9 10; do echo 'waiting for lock'; done; echo done"}`)
	if got := strings.Count(out, "waiting for lock"); got != 1 {
		t.Fatalf("%d copies of the repeated line, want 1:\n%s", got, out)
	}
	if !strings.Contains(out, "[previous line repeated 9 more times]") {
		t.Fatalf("no repeat count:\n%s", out)
	}
	if !strings.Contains(out, "done") {
		t.Fatalf("the lines after the run were lost:\n%s", out)
	}
}

func TestASilentCommandThatSucceededSaysSo(t *testing.T) {
	out, err := NewExec(t.TempDir()).Run(context.Background(), json.RawMessage(`{"command":"true"}`))
	if err != nil {
		t.Fatal(err)
	}
	// an empty answer reads as "unknown" to a small model, which then runs the build again
	if !strings.Contains(out, "exit 0") {
		t.Errorf("a silent success answered %q", out)
	}
}

// what a command that prints far past its budget costs the run: a window, not the print
func BenchmarkAChattyCommand(b *testing.B) {
	ex := NewExec(b.TempDir())
	ex.MaxBytes = 4096
	ex.LogDir = b.TempDir()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ex.Run(context.Background(), json.RawMessage(`{"command":"seq 1 200000"}`)); err != nil {
			b.Fatal(err)
		}
	}
}
