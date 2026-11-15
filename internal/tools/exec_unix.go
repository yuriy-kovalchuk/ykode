//go:build unix

package tools

import (
	"os/exec"
	"syscall"
)

// groupCommand runs the command in its own process group so cancelling kills children, not just sh.
// The group is what makes the kill reach a build's compiler processes and a test runner's subprocesses:
// killing only the shell leaves them writing into pipes nobody reads.
func groupAttributes() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// killGroup takes the whole group down. The leading minus on the pid is what says "the group", and
// SIGKILL because a child that will not stop on request has had its chance.
func killGroup(cmd *exec.Cmd) error {
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
