package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// sandboxArgs confines a command to writing the project, temp and the user's cache, offline; no sandbox, no run.
func sandboxArgs(root, command string) ([]string, error) {
	writable := []string{os.TempDir(), "/tmp"} // commands get no TMPDIR, so their tools fall back to /tmp
	if cache, err := os.UserCacheDir(); err == nil {
		writable = append(writable, cache)
	}
	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("sandbox-exec"); err != nil {
			return nil, fmt.Errorf("exec_command: the sandbox is on but sandbox-exec is missing")
		}
		return []string{"sandbox-exec", "-p", seatbeltProfile(append([]string{root}, writable...)), "sh", "-c", command}, nil
	case "linux":
		if _, err := exec.LookPath("bwrap"); err != nil {
			return nil, fmt.Errorf("exec_command: the sandbox is on but bwrap is missing; install bubblewrap")
		}
		return bwrapArgs(root, writable, command), nil
	}
	return nil, fmt.Errorf("exec_command: no sandbox for %s", runtime.GOOS)
}

// seatbeltProfile denies the network and writes outside the folders, resolved as macOS checks real paths.
func seatbeltProfile(writable []string) string {
	var allow strings.Builder
	for _, dir := range writable {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			dir = real
		}
		fmt.Fprintf(&allow, " (subpath %q)", dir)
	}
	return `(version 1) (allow default) (deny network*) (deny file-write*)
(allow file-write*` + allow.String() + ` (literal "/dev/null") (literal "/dev/tty") (subpath "/dev/fd"))`
}

// bwrapArgs gives the command a read-only view of the system, the writable folders, and no network.
func bwrapArgs(root string, writable []string, command string) []string {
	args := []string{"bwrap", "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--bind", root, root}
	for _, dir := range writable {
		args = append(args, "--bind", dir, dir)
	}
	return append(args, "--unshare-net", "--die-with-parent", "--chdir", root, "sh", "-c", command)
}
