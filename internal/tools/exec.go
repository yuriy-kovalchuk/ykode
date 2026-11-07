package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// Exec runs sh -c in the project root: confined by the working directory, not by the file sandbox.
type Exec struct {
	dir      string
	last     Detail        // what the last command exited with and where its full output went
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

func (Exec) Description() string {
	return fmt.Sprintf("Run a shell command with sh -c in the project root; you get cleaned output (no escape sequences or progress spam) plus the exit code. Default timeout %ds, max %d minutes. Cut output is saved to a log file whose path is shown: read or grep that instead of re-running.", int(defaultExecTimeout.Seconds()), int(maxExecTimeout.Minutes()))
}

func (Exec) NeedsConfirm() bool { return true }

// LastDetail reports the exit code and saved log of the command that just ran.
func (t *Exec) LastDetail() Detail { return t.last }

// exitCode reads the exit status of a finished command, if it reports one.
func exitCode(err error) (int, bool) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true
	}
	return 0, false
}

// Targets reports no paths: a shell command has no single path to confirm.
func (Exec) Targets(json.RawMessage) ([]Target, error) { return nil, nil }

func (Exec) Schema() json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{
	  "type": "object",
	  "properties": {
	    "command": {"type": "string", "description": "shell command line"},
	    "timeout": {"type": "integer", "description": "seconds before the command is killed (default %d, max %d)"}
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

	cmd := groupCommand(runCtx, "sh", "-c", in.Command)
	cmd.Dir = t.dir
	cmd.Env = commandEnv()
	out, err := cmd.CombinedOutput()
	result := cleanOutput(string(out))
	t.last = Detail{}
	if code, ok := exitCode(err); ok {
		t.last.ExitCode = &code
	}
	if err == nil {
		return t.fit(result), nil
	}
	// a non-zero exit is a normal result: the model needs the command's own output to fix it
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		t.last.TimedOut = true
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
	return t.fit(result), nil
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

// fit keeps output inside the budget (head plus most of the tail) and spills the full log to a file.
// aim is the byte ceiling for this command's output, with room left for the note about the cut.
func (t Exec) aim() int {
	cap := t.MaxBytes
	if cap <= 0 {
		cap = defaultResultBytes
	}
	return cap - budgetSlack
}

func (t *Exec) fit(out string) string {
	budget := t.aim()
	if len(out) <= budget {
		return out
	}
	path, err := writeFullOutput(t.logDir(), out)
	head, tail := cutUTF8(out, budget/budgetSlackDiv), cutUTF8Tail(out, budget-budget/budgetSlackDiv)
	var b strings.Builder
	b.WriteString(head)
	fmt.Fprintf(&b, "\n\n[... %d of %d bytes omitted ...", len(out)-len(head)-len(tail), len(out))
	if err != nil {
		fmt.Fprintf(&b, " (could not save the full output: %v]", err)
	} else {
		t.last.LogPath = path
		fmt.Fprintf(&b, " full output: %s (read it with exec_command, e.g. sed -n '1,80p' FILE)]", path)
	}
	b.WriteString("\n\n")
	b.WriteString(tail)
	return b.String()
}

// PruneSpillLogs deletes whole command logs older than maxAge.
func PruneSpillLogs(dir string, maxAge time.Duration) (int, error) {
	return PruneOldDirs(dir, spillPrefix, maxAge)
}

// writeFullOutput stores an oversized output in a temp dir and returns its path.
func writeFullOutput(dir, out string) (string, error) {
	dir, err := os.MkdirTemp(dir, spillPrefix)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "output.log")
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
