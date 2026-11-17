package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// cancelling must take a command's children with it, which is why it runs in its own group
func TestACancelledCommandTakesItsChildrenWithIt(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "child")
	tool := NewExec(t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()

	// the child is a shell of its own, so it only dies if the kill reaches the group
	args := fmt.Sprintf(`{"command":"sh -c 'sleep 30 & echo $! > %s; wait'"}`, marker)
	_, err := tool.Run(ctx, json.RawMessage(args))
	if err == nil {
		t.Log("the command reported no error; checking the child anyway")
	}

	raw, readErr := os.ReadFile(marker)
	if readErr != nil {
		t.Skipf("the child never wrote its id: %v", readErr)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("the marker holds %q, want a process id", string(raw))
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		groupGone, processGone := syscall.Kill(-pid, 0) != nil, syscall.Kill(pid, 0) != nil
		if groupGone && processGone {
			return // neither the child nor a group it leads is there any more
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("the child pid %d outlived the cancelled command", pid)
}

// the group request is the point: a bare SysProcAttr leaves the cancel reaching only the shell
func TestACommandAsksForItsOwnProcessGroup(t *testing.T) {
	cmd := groupCommand(context.Background(), "sh", "-c", "true")
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.Setpgid {
		t.Errorf("SysProcAttr = %+v, want Setpgid set so children are taken down with the command", cmd.SysProcAttr)
	}
}
