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
	branch := GitBranch(root)
	if branch == "" {
		return "not a git repository"
	}
	if changes := gitOut(root, "status", "--porcelain"); changes != "" {
		return "branch " + branch + ", uncommitted changes"
	}
	return "branch " + branch + ", clean"
}

// GitBranch is what HEAD points at in root, or "" when root is not a git repository. The screen
// asks for it each turn; the prompt asked for it once, when it was built.
func GitBranch(root string) string {
	if root == "" {
		return ""
	}
	return gitOut(root, "rev-parse", "--abbrev-ref", "HEAD")
}

// gitOut runs one git command in root and returns its trimmed output, "" when it fails.
func gitOut(root string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
