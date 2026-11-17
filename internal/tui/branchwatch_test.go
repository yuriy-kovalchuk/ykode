package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
)

// shortTemp is a named temporary dir: the footer truncates a long path before the branch
func shortTemp(t *testing.T, mark string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", mark)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// gitProject is a project with one commit, on a branch of its own, and a change waiting in the tree.
func gitProject(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := shortTemp(t, "p")
	git(t, dir, "init", "-q", "-b", "footer")
	git(t, dir, "config", "user.email", "ykode@example.com")
	git(t, dir, "config", "user.name", "ykode")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "start")
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("work"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// modelIn is a screen for a project on disk, wide enough for the footer's left side.
func modelIn(t *testing.T, dir string) *model {
	t.Helper()
	a := agent.New(nil, nil, "test-model", agent.Options{Root: dir}, nil)
	m := newModel(context.Background(), a, NewHub(), config.Config{})
	m.width, m.height = 100, 24
	return m
}

func TestTheFooterFollowsACheckoutMadeElsewhere(t *testing.T) {
	dir := gitProject(t)
	m := modelIn(t, dir)
	if out := stripCodes(m.View()); !strings.Contains(out, "(footer)") {
		t.Fatalf("the footer does not name the branch it started on:\n%s", out)
	}

	git(t, dir, "checkout", "-q", "-b", "elsewhere")
	m.Update(branchTickMsg{})

	if out := stripCodes(m.View()); !strings.Contains(out, "(elsewhere)") {
		t.Errorf("the footer still names the old branch after a checkout elsewhere:\n%s", out)
	}
}

// a look that found nothing to say must not draw the session again: a reader scrolled up stays put
func TestALookThatFoundNoChangeLeavesTheScreenAlone(t *testing.T) {
	m := modelIn(t, gitProject(t))
	before := stripCodes(m.View())

	m.Update(branchTickMsg{})

	if got := stripCodes(m.View()); got != before {
		t.Errorf("a quiet look redrew the screen:\n%s\nwant\n%s", got, before)
	}
}

// a worktree keeps a .git file, so its own HEAD is the file that moves and shows its branch
func TestTheFooterFollowsAWorktree(t *testing.T) {
	dir := gitProject(t)
	work := filepath.Join(shortTemp(t, "w"), "side")
	git(t, dir, "worktree", "add", "-q", work, "-b", "side")

	m := modelIn(t, work)
	if out := stripCodes(m.View()); !strings.Contains(out, "(side)") {
		t.Fatalf("the footer does not name the worktree's branch:\n%s", out)
	}

	git(t, work, "checkout", "-q", "-b", "elsewhere")
	m.Update(branchTickMsg{})

	if out := stripCodes(m.View()); !strings.Contains(out, "(elsewhere)") {
		t.Errorf("the footer did not follow the worktree's checkout:\n%s", out)
	}
}

// a project with no repository has no branch to follow and no HEAD to look at
func TestAProjectOutsideARepositoryIsLeftAlone(t *testing.T) {
	m := modelIn(t, shortTemp(t, "bare"))
	if got := headFile(m.root); got != "" {
		t.Errorf("headFile of a directory with no repository = %q, want nothing", got)
	}

	m.Update(branchTickMsg{})

	if m.branch != "" {
		t.Errorf("the footer invented branch %q for a directory with no repository", m.branch)
	}
}

// git runs one command in dir and fails the test when it cannot
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", args, err, out)
	}
}
