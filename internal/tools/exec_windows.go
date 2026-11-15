//go:build !unix

package tools

import (
	"os/exec"
	"syscall"
)

// groupAttributes carries no process-group request: the operating system has no setpgid, so a command
// runs as its own process tree and the kill below reaches the shell it was started as. Children of a
// build or a test runner are left to their own exit, which is why this platform is supported for
// running ykode rather than for confining what ykode runs.
func groupAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

// killGroup stops the command. Without a process group this is the process itself; the wait that
// follows bounds how long an unread pipe can hold the command open.
func killGroup(cmd *exec.Cmd) error {
	return cmd.Process.Kill()
}
