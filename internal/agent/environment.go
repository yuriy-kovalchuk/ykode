package agent

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// withEnvironment ends a prompt with where the session runs, last so the fixed rules stay a cached
// prefix. Only what holds for the whole session is here: the project, where a command starts, and the
// system. What changes between one run and the next (the date, and whether the tree has changes) is not,
// because a server's cache matches from the first token: one word different here and everything after it,
// the tool definitions and the whole conversation, is read again. That news travels with the ask instead
// (see environmentNote).
func withEnvironment(prompt, root string) string {
	return prompt + "\n<environment>\nproject root: " + root +
		"\nworking directory: " + root + " (exec_command starts here, so a command needs no cd, and a relative path is from here)" +
		"\nos: " + runtime.GOOS + "/" + runtime.GOARCH +
		"\nshell for exec_command: sh\n</environment>\n"
}

// environmentNote is the date and the state of the git tree as of an ask, written as the tail of the ask
// itself. A note in the system prompt would move the cached prefix whenever a commit or a new day
// changed it; here it costs a few tokens of the newest message and nothing of the history before it.
func environmentNote(root string, now time.Time) string {
	state, _ := gitState(root)
	return "date: " + now.Format("2006-01-02") + "\ngit: " + state
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
