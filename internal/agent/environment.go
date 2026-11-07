package agent

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// withEnvironment ends a prompt with where the session runs, last so the fixed rules stay a cached prefix.
func withEnvironment(prompt, root string) string {
	return prompt + "\n<environment>\nproject root: " + root +
		"\nos: " + runtime.GOOS + "/" + runtime.GOARCH +
		"\nshell for exec_command: sh\ndate: " + time.Now().Format("2006-01-02") +
		"\ngit: " + gitState(root) + "\n</environment>\n"
}

// gitState is the branch and whether the tree has changes, or that there is no repository.
func gitState(root string) string {
	if root == "" {
		return "unknown"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	git := func(args ...string) (string, error) {
		out, err := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...).Output()
		return strings.TrimSpace(string(out)), err
	}
	branch, err := git("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "not a git repository"
	}
	if changes, _ := git("status", "--porcelain"); changes != "" {
		return "branch " + branch + ", uncommitted changes"
	}
	return "branch " + branch + ", clean"
}
