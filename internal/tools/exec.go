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
	// execOutputBytes bounds what the model sees; the whole log goes to a file
	// so a cut-off build error is still reachable. Build and test failures live
	// at the end of the output, so most of the budget is spent there.
	execHeadBytes = resultBudget / 4
	execTailBytes = resultBudget - execHeadBytes
	// spillPrefix names the temp directories that hold a cut command's full log.
	spillPrefix = "ykode-exec-"
	// waitDelay bounds the wait when a command exits but leaves its output
	// pipes open (a daemonised grandchild), or refuses to die after a cancel.
	waitDelay = 5 * time.Second
)

// commandEnv is the entire environment a shell command receives. Inheriting
// ykode's environment would hand the model whatever token is kept there, so
// only what builds, git and terminals genuinely need gets through.
func commandEnv() []string {
	env := make([]string, 0, 4)
	for _, key := range []string{"PATH", "HOME", "TERM", "LANG"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	return env
}

// Exec runs a shell command in the project root. Note: it runs through sh, so
// it is only confined by the working directory, not by the file sandbox.
type Exec struct{ dir string }

func NewExec(dir string) *Exec { return &Exec{dir: dir} }

func (Exec) Name() string { return "exec_command" }

func (Exec) Description() string {
	return fmt.Sprintf("Run a shell command with sh -c in the project root. stdout and stderr are combined. Use it for builds, tests, git. Times out after %d seconds by default; pass timeout for longer work (max %d). If the output is cut, the full log path is reported.", int(defaultExecTimeout.Seconds()), int(maxExecTimeout.Seconds()))
}

func (Exec) NeedsConfirm() bool { return true }

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

func (t Exec) Run(ctx context.Context, args json.RawMessage) (string, error) {
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
	timeout, capped := execTimeout(in.Timeout)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := groupCommand(runCtx, "sh", "-c", in.Command)
	cmd.Dir = t.dir
	cmd.Env = commandEnv()
	out, err := cmd.CombinedOutput()
	result := string(out)
	if err == nil {
		return t.fit(result), nil
	}
	// A non-zero exit is a normal result: the model needs the command's own
	// output to fix it, so report it in the text instead of losing it.
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
	return t.fit(result), nil
}

// execTimeout clamps the model's request into a sane range.
func execTimeout(seconds int) (time.Duration, bool) {
	if seconds <= 0 {
		return defaultExecTimeout, false
	}
	if seconds > int(maxExecTimeout.Seconds()) {
		return maxExecTimeout, true
	}
	return time.Duration(seconds) * time.Second, false
}

// groupCommand runs a command in its own process group and kills the whole
// group when the context ends: sh and rg exit, but their children (a build, a
// dev server) would otherwise outlive the cancellation.
func groupCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = waitDelay
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	return cmd
}

// fit keeps the output the model sees inside the tool budget, keeping the head
// and most of the tail, and spills the full log to a file it can read back.
func (t Exec) fit(out string) string {
	if len(out) <= resultBudget {
		return out
	}
	path, err := writeFullOutput(out)
	head, tail := cutUTF8(out, execHeadBytes), cutUTF8Tail(out, execTailBytes)
	var b strings.Builder
	b.WriteString(head)
	fmt.Fprintf(&b, "\n\n[... %d of %d bytes omitted ...", len(out)-len(head)-len(tail), len(out))
	if err != nil {
		fmt.Fprintf(&b, " (could not save the full output: %v]", err)
	} else {
		fmt.Fprintf(&b, " full output: %s (read it with exec_command, e.g. sed -n '1,80p' FILE)]", path)
	}
	b.WriteString("\n\n")
	b.WriteString(tail)
	return b.String()
}

// writeFullOutput stores a long command output under the project's temp dir and
// returns its path. Nothing deletes it: the OS clears temp directories, and the
// model may need the log after the turn that produced it.
// PruneSpillLogs deletes command output logs older than maxAge from dir and
// reports how many went. They are written one per oversized command and nothing
// else cleans them up.
func PruneSpillLogs(dir string, maxAge time.Duration) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("clean old command logs: %w", err)
	}
	removed := 0
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), spillPrefix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return removed, fmt.Errorf("stat %s: %w", e.Name(), err)
		}
		if time.Since(info.ModTime()) < maxAge {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return removed, fmt.Errorf("remove %s: %w", e.Name(), err)
		}
		removed++
	}
	return removed, nil
}

func writeFullOutput(out string) (string, error) {
	dir, err := os.MkdirTemp("", spillPrefix)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "output.log")
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
