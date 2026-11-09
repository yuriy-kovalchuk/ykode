package agent

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// withEnvironment ends a prompt with where the session runs, last so the fixed rules stay a cached
// prefix. It hands back the branch as well: the screen names that too, and asking git again at
// startup is time before anything is drawn.
func withEnvironment(prompt, root string) (text, branch string) {
	state, branch := gitState(root)
	return prompt + "\n<environment>\nproject root: " + root +
		"\nworking directory: " + root + " (exec_command starts here, so a command needs no cd, and a relative path is from here)" +
		"\nos: " + runtime.GOOS + "/" + runtime.GOARCH +
		"\nshell for exec_command: sh\ndate: " + time.Now().Format("2006-01-02") +
		"\ngit: " + state + "\n</environment>\n", branch
}

// gitState is the branch and whether the tree has changes, or that there is no repository. The two
// questions are asked at once: a startup that waits for git one after another waits twice.
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
