package tui

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// branchLook is how often the screen asks whether HEAD moved. The question costs one stat, so a
// shorter wait buys nothing back but a few syscalls; 5s is well inside how long a reader looks at
// the window where they ran the checkout before glancing back here.
const branchLook = 5 * time.Second

// headWatch is where a project's HEAD file is, and how it looked when it was last looked at.
type headWatch struct {
	path string
	when time.Time
	size int64
}

// watchHead points at the HEAD of the repository holding root, and records how it looks now: the
// first look is not a change.
func watchHead(root string) headWatch {
	h := headWatch{path: headFile(root)}
	h.moved()
	return h
}

// moved says HEAD changed since the last look, and remembers how it looks now. The file is named,
// not opened: git writes HEAD by renaming a new file over it, so a watch on the old inode goes
// quiet, while a stat follows the name.
func (h *headWatch) moved() bool {
	info, err := os.Stat(h.path)
	if err != nil {
		return false
	}
	moved := !info.ModTime().Equal(h.when) || info.Size() != h.size
	h.when, h.size = info.ModTime(), info.Size()
	return moved
}

// headFile is the file that says which branch root has, walking up until it finds a repository:
// .git/HEAD, or the one in the directory a worktree's "gitdir: …" names - the .git there is a file
// that never changes, so watching it would miss every checkout.
func headFile(root string) string {
	for dir := root; dir != ""; dir = parent(dir) {
		git := filepath.Join(dir, ".git")
		info, err := os.Stat(git)
		switch {
		case err != nil:
			continue
		case info.IsDir():
			return filepath.Join(git, "HEAD")
		}
		body, err := os.ReadFile(git)
		if err != nil {
			return ""
		}
		gitdir := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(body)), "gitdir:"))
		if !filepath.IsAbs(gitdir) {
			gitdir = filepath.Join(dir, gitdir)
		}
		return filepath.Join(gitdir, "HEAD")
	}
	return ""
}

// parent is the directory above dir, and "" at the top of the tree, so the walk up stops there.
func parent(dir string) string {
	if up := filepath.Dir(dir); up != dir {
		return up
	}
	return ""
}

// watchBranch asks again, one branchLook from now, whether HEAD moved.
func (m *model) watchBranch() tea.Cmd {
	return tea.Tick(branchLook, func(time.Time) tea.Msg { return branchTickMsg{} })
}

// checkBranch moves the footer when HEAD moved: a checkout in another window, or one the run itself
// made, which the look at the start of a prompt is too early to see. git is still asked for the
// name, so a branch is only looked up when something says to, and the screen is only drawn again
// when the name really differs.
func (m *model) checkBranch() {
	if !m.head.moved() {
		return
	}
	if next := agent.GitBranch(m.root); next != m.branch {
		m.branch = next
		m.refresh()
	}
}
