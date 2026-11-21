package agent

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// withEnvironment ends a prompt with where the session runs, last so the fixed rules stay cached
func withEnvironment(prompt, root string, runs bool) string {
	if !runs { // with no exec_command the lines about where it starts and what shell it gets point at nothing
		return prompt + "\n<environment>\nproject root: " + root + "\nos: " + runtime.GOOS + "/" + runtime.GOARCH + "\n</environment>\n"
	}
	return prompt + "\n<environment>\nproject root: " + root +
		"\nworking directory: " + root + " (exec_command starts here, so a command needs no cd, and a relative path is from here)" +
		"\nos: " + runtime.GOOS + "/" + runtime.GOARCH +
		"\nshell for exec_command: sh\n</environment>\n"
}

// environmentNote is the date and git state as the tail of the ask, costing no cached prefix
func environmentNote(root string, now time.Time) string {
	state, _ := gitState(root)
	return "date: " + now.Format("2006-01-02") + "\ngit: " + state
}

// gitState is the branch and whether the tree has changes, asked in one command, not two
func gitState(root string) (state, branch string) {
	if root == "" {
		return "unknown", ""
	}
	head := make(chan string, 1)
	go func() { head <- GitBranch(root) }()
	changes := gitOut(root, "status", "--porcelain")
	branch = <-head
	switch {
	case branch == "":
		return "not a git repository", ""
	case changes != "":
		return "branch " + branch + ", uncommitted changes", branch
	}
	return "branch " + branch + ", clean", branch
}

// GitBranch is what HEAD points at, or "" with no repository; the screen asks it each turn
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
