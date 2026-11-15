package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const (
	defaultExecTimeout = 60 * time.Second
	maxExecTimeout     = 10 * time.Minute
	// the head is short because build and test failures live at the end of the output
	budgetSlackDiv = 4 // the head is a quarter of the budget
	// spillPrefix names the temp directories that hold a cut command's full log.
	spillPrefix = "ykode-exec-"
	// waitDelay bounds the wait when a command exits but leaves its output pipes open
	waitDelay = 5 * time.Second
)

// commandEnv keeps the shell environment minimal so a token in ykode's env never reaches the model's commands.
func commandEnv() []string {
	env := make([]string, 0, 4)
	for _, key := range []string{"PATH", "HOME", "TERM", "LANG"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	return env
}

// Exec runs sh -c in the project root: confined by the working directory, not by the file tools' rules.
type Exec struct {
	dir      string
	Timeout  time.Duration // kill after this when the model does not ask; 0 means defaultExecTimeout
	LogDir   string        // where a cut command's full output is saved; empty means os.TempDir()
	MaxBytes int           // bytes the result may use; 0 means defaultResultBytes
}

func (t Exec) timeout() time.Duration {
	if t.Timeout > 0 {
		return t.Timeout
	}
	return defaultExecTimeout
}

func NewExec(dir string) *Exec { return &Exec{dir: dir} }

// logDir is where cut command output lands, created on demand.
func (t Exec) logDir() string {
	base := t.LogDir
	if base == "" {
		base = os.TempDir()
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return os.TempDir()
	}
	return base
}

func (Exec) Name() string { return "exec_command" }
func (Exec) Class() Class { return ClassRuns }

func (Exec) Description() string {
	return "Run a shell command (sh -c on " + runtime.GOOS + ") in the project root; returns the output and exit code. A cut output names the file with the full log. " +
		"Filter in the command itself (grep, awk, a short script) so only what answers the question comes back."
}

func (Exec) NeedsConfirm() bool { return true }

// Targets reports no paths: a shell command has no single path to confirm.
func (Exec) Targets(json.RawMessage) ([]Target, error) { return nil, nil }

func (Exec) Schema() json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{
	  "type": "object",
	  "properties": {
	    "command": {"type": "string"},
	    "timeout": {"type": "integer", "description": "seconds (default %d, max %d)"}
	  },
	  "required": ["command"]
	}`, int(defaultExecTimeout.Seconds()), int(maxExecTimeout.Seconds())))
}

func (t *Exec) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	if strings.TrimSpace(in.Command) == "" {
		return "", fmt.Errorf("exec_command: command is empty")
	}
	timeout, capped := execTimeout(in.Timeout, t.timeout())
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	argv := []string{"sh", "-c", in.Command}
	cmd := groupCommand(runCtx, argv[0], argv[1:]...)
	cmd.Dir = t.dir
	cmd.Env = commandEnv()
	// the output arrives through a window: what the model sees stays in RAM, and the whole of it is
	// written to the log as the command makes it
	window := newOutWindow(t.aim(), t.logDir())
	cmd.Stdout, cmd.Stderr = window, window
	err := cmd.Run()
	window.close()
	result := t.fit(window)
	if err == nil {
		if strings.TrimSpace(result) == "" {
			return "exit 0, no output: the command succeeded", nil // silence reads as "unknown" and invites a re-run
		}
		return result, nil
	}
	// a non-zero exit is a normal result: the model needs the command's own output to fix it
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		result += fmt.Sprintf("\n[command killed after its %s timeout; pass a larger timeout if this is expected work]", timeout)
	case runCtx.Err() != nil:
		result += "\n[cancelled]"
	case errors.Is(err, exec.ErrWaitDelay):
		result += "\n[command exited but kept the output pipes open; killed]"
	default:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return "", toolError(t.Name(), in.Command, err)
		}
		result += fmt.Sprintf("\n[exit status %d]", exitErr.ExitCode())
	}
	if capped {
		result += fmt.Sprintf("\n[timeout was capped at %s]", maxExecTimeout)
	}
	return result, nil
}

// execTimeout clamps the model's request into a sane range.
func execTimeout(seconds int, def time.Duration) (time.Duration, bool) {
	if seconds <= 0 {
		return def, false
	}
	if seconds > int(maxExecTimeout.Seconds()) {
		return maxExecTimeout, true
	}
	return time.Duration(seconds) * time.Second, false
}

// groupCommand runs the command in its own process group so cancelling kills children, not just sh.
func groupCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = waitDelay
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	return cmd
}

// fit is what the model sees of a command's output: its head, most of its tail, and where the whole
// of it was written. The counts are the command's own bytes, before the output was cleaned.
func (t *Exec) fit(w *outWindow) string {
	if w.total <= w.goal {
		return cleanOutput(string(w.head) + string(w.tail))
	}
	head, tail := cleanOutput(headText(w.head)), cleanOutput(tailText(w.tail))
	var b strings.Builder
	b.WriteString(head)
	fmt.Fprintf(&b, "\n\n[... %d of %d bytes omitted ...", w.total-len(w.head)-len(w.tail), w.total)
	if w.why != nil {
		fmt.Fprintf(&b, " (could not save the full output: %v]", w.why)
	} else {
		fmt.Fprintf(&b, " full output: %s (read it with exec_command, e.g. sed -n '1,80p' FILE)]", w.path)
	}
	b.WriteString("\n\n")
	b.WriteString(tail)
	return b.String()
}

// aim is the byte ceiling for this command's output, with room left for the note about the cut. It
// carries the same floor as the file tools' ceiling: a budget smaller than the note would leave every
// cut below asking for a negative length.
func (t Exec) aim() int {
	cap := t.MaxBytes
	if cap <= 0 {
		cap = defaultResultBytes
	}
	return max(cap-budgetSlack, minAimBytes)
}

// PruneSpillLogs deletes whole command logs older than maxAge, and reports how many went. One entry
// that cannot be read or removed is passed over rather than ending the sweep: it is one folder among
// many, and stopping here would leave everything after it on disk for every future start.
func PruneSpillLogs(dir string, maxAge time.Duration) (int, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil // a first run has nothing to clean
	}
	if err != nil {
		return 0, fmt.Errorf("clean old files in %s: %w", dir, err)
	}
	removed, skipped := 0, 0
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), spillPrefix) {
			continue
		}
		info, err := e.Info()
		if err != nil || time.Since(info.ModTime()) < maxAge {
			if err != nil {
				skipped++
			}
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			skipped++
			continue
		}
		removed++
	}
	if skipped > 0 {
		return removed, fmt.Errorf("%d old log folder(s) in %s could not be read or removed", skipped, dir)
	}
	return removed, nil
}
