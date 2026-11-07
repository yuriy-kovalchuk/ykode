package tools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func runSandboxed(t *testing.T, dir, command string) string {
	t.Helper()
	run := NewExec(dir)
	run.Sandbox = true
	args, _ := json.Marshal(map[string]string{"command": command})
	out, err := run.Run(context.Background(), args)
	if err != nil {
		return err.Error()
	}
	return out
}

func TestTheSandboxKeepsWritesInsideTheProjectAndTheNetworkOff(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the macOS profile is what this machine can run")
	}
	dir := t.TempDir()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(home, ".ykode-sandbox-probe")
	t.Cleanup(func() { os.Remove(outside) })

	if out := runSandboxed(t, dir, "echo inside > inside.txt"); strings.Contains(out, "exit status") {
		t.Fatalf("a write inside the project was refused: %s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "inside.txt")); err != nil {
		t.Error("the write inside the project did not happen")
	}
	runSandboxed(t, dir, "echo leaked > "+outside)
	if _, err := os.Stat(outside); err == nil {
		t.Error("a sandboxed command wrote outside the project")
	}
	if _, err := exec.LookPath("curl"); err == nil {
		if out := runSandboxed(t, dir, "curl -s -m 3 -o /dev/null -w '%{http_code}' http://1.1.1.1"); !strings.Contains(out, "exit status") {
			t.Errorf("a sandboxed command reached the network: %s", out)
		}
	}
}

func TestBwrapGetsTheSameRules(t *testing.T) {
	args := bwrapArgs("/work/proj", []string{"/tmp", "/home/u/.cache"}, "go test ./...")
	for _, want := range [][]string{{"--ro-bind", "/", "/"}, {"--bind", "/work/proj", "/work/proj"}, {"--bind", "/home/u/.cache", "/home/u/.cache"}, {"--unshare-net"}, {"sh", "-c", "go test ./..."}} {
		if !containsRun(args, want) {
			t.Errorf("bwrap args %v miss %v", args, want)
		}
	}
}

func TestASandboxThatCannotStartRefusesTheCommand(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // neither sandbox-exec nor bwrap can be found
	if out := runSandboxed(t, t.TempDir(), "echo hi"); !strings.Contains(out, "sandbox") {
		t.Errorf("the command ran unconfined: %q", out)
	}
}

// containsRun says want appears in args as consecutive elements.
func containsRun(args, want []string) bool {
	for i := range args {
		if i+len(want) <= len(args) && slices.Equal(args[i:i+len(want)], want) {
			return true
		}
	}
	return false
}
