package tui

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// branchLook is how often the screen asks whether HEAD moved; the question costs one stat
const branchLook = 5 * time.Second

// headWatch is where a project's HEAD file is, and how it looked when it was last looked at.
type headWatch struct {
	path string
	when time.Time
	size int64
}

// watchHead points at the HEAD of root's repository and records how it looks now
func watchHead(root string) headWatch {
	h := headWatch{path: headFile(root)}
	h.moved()
	return h
}

// moved says HEAD changed since the last look: it stats the name, since git renames over HEAD
func (h *headWatch) moved() bool {
	info, err := os.Stat(h.path)
	if err != nil {
		return false
	}
	moved := !info.ModTime().Equal(h.when) || info.Size() != h.size
	h.when, h.size = info.ModTime(), info.Size()
	return moved
}

// headFile is the file naming root's branch, walking up past a worktree's gitdir file
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

// checkBranch moves the footer when HEAD moved, asking git for the name only when told to
func (m *model) checkBranch() {
	if !m.head.moved() {
		return
	}
	if next := agent.GitBranch(m.root); next != m.branch {
		m.branch = next
		m.refresh()
	}
}
