package tools

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Target is a path a tool is about to touch, resolved, plus whether it lies outside the project root.
type Target struct {
	Raw      string
	Resolved string
	Outside  bool
}

// resolveTarget makes a model-supplied path absolute; outside paths are reported to the agent, not rejected.
func resolveTarget(raw, root string) Target {
	p := strings.TrimSpace(raw)
	var abs string
	switch {
	case p == "" || p == ".":
		abs = root
	case filepath.IsAbs(p):
		abs = filepath.Clean(p)
	case rootWithoutSlash(p, root):
		// a model copying the root from its environment sometimes drops the leading slash
		abs = filepath.Clean(string(filepath.Separator) + p)
	default:
		abs = filepath.Clean(filepath.Join(root, p))
	}
	return Target{Raw: raw, Resolved: abs, Outside: !within(root, abs)}
}

// rootWithoutSlash says p is the absolute root, or a path under it, written without its leading slash.
func rootWithoutSlash(p, root string) bool {
	if !filepath.IsAbs(root) {
		return false
	}
	bare := strings.TrimPrefix(filepath.Clean(root), string(filepath.Separator))
	if bare == "" {
		return false // the root is "/" itself: every relative path would match
	}
	return p == bare || strings.HasPrefix(p, bare+string(filepath.Separator))
}

func within(root, path string) bool {
	if path == root {
		return true
	}
	sep := string(filepath.Separator)
	if !strings.HasSuffix(root, sep) {
		root += sep
	}
	return strings.HasPrefix(path, root)
}

// scope is the file handle the file tools share: the project Root plus this call's outside approvals.
type scope struct {
	root   *os.Root
	dir    string // absolute project root
	budget int    // bytes one result may use; 0 means defaultResultBytes
	undo   *UndoStack
}

// cap is the byte ceiling for this tool's result.
func (s scope) cap() int {
	if s.budget > 0 {
		return s.budget
	}
	return defaultResultBytes
}

// aim leaves room for the note that explains a cut.
func (s scope) aim() int { return s.cap() - budgetSlack }

// handle addresses one target as root+name, and releases anything opened for it.
type handle struct {
	root   *os.Root
	name   string
	target Target
	base   string   // directory the Root sits at; empty for the project Root
	closer *os.Root // a Root opened for an outside path; nil for in-project ones
}

func (h handle) close() {
	if h.closer != nil {
		h.closer.Close()
	}
}

// display renders a handle-relative path so the model can hand it straight back to another tool.
func (h handle) display(rel string) string {
	if h.base == "" {
		return rel
	}
	return h.base + "/" + rel
}

// shown is the handle's own path as the model should repeat it: project-relative inside the root.
func (h handle) shown() string { return h.display(h.name) }

// open resolves raw into a Root and a name inside it, giving an approved outside path its own Root.
func (s scope) open(ctx context.Context, raw string) (handle, error) {
	t := resolveTarget(raw, s.dir)
	if !t.Outside {
		rel, err := filepath.Rel(s.dir, t.Resolved)
		if err != nil {
			return handle{}, fmt.Errorf("path %q: %w", raw, err)
		}
		return handle{root: s.root, name: rel, target: t}, nil
	}
	if !approved(ctx, t.Resolved) {
		return handle{}, fmt.Errorf("%q is outside the project root (%s) and was not approved", t.Resolved, s.dir)
	}
	dir, rel := nearestExistingDir(t.Resolved)
	outer, err := os.OpenRoot(dir)
	if err != nil {
		return handle{}, fmt.Errorf("open %q: %w", dir, err)
	}
	return handle{root: outer, name: rel, target: t, base: dir, closer: outer}, nil
}

// nearestExistingDir walks up to the first directory that exists, with the target named relative to it.
func nearestExistingDir(path string) (dir, rel string) {
	dir = filepath.Dir(path)
	for {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			if r, err := filepath.Rel(dir, path); err == nil {
				return dir, r
			}
			return dir, filepath.Base(path)
		}
		parent := filepath.Dir(dir)
		if parent == dir { // reached the filesystem root
			return dir, filepath.Base(path)
		}
		dir = parent
	}
}

type approvalsKey struct{}

// WithApprovals grants outside paths for one call only; nothing is remembered between calls.
func WithApprovals(ctx context.Context, paths []string) context.Context {
	return context.WithValue(ctx, approvalsKey{}, paths)
}

func approved(ctx context.Context, path string) bool {
	granted, _ := ctx.Value(approvalsKey{}).([]string)
	return slices.Contains(granted, path)
}

// absolute joins a Root-relative path onto the directory its Root sits at.
func absolute(dir, rel string) string {
	return filepath.Join(dir, filepath.FromSlash(rel))
}

// pushUndo remembers a file as it is now, so /undo can put it back after this call changes it.
func (s scope) pushUndo(h handle, prev, next []byte, existed bool) {
	if s.undo == nil {
		return
	}
	dir := s.dir
	if h.target.Outside {
		dir, _ = outsideDir(h.target.Resolved)
	}
	s.undo.push(Change{
		dir:     dir,
		name:    h.name,
		shown:   h.shown(),
		prev:    prev,
		wrote:   sha256.Sum256(next),
		existed: existed,
	})
}
