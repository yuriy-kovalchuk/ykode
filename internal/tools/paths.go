package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Target is a path a tool is about to touch, resolved to an absolute path,
// along with whether it lies outside the project root.
type Target struct {
	Raw      string
	Resolved string
	Outside  bool
}

// resolveTarget makes a model-supplied path absolute. Unlike the earlier
// sandbox, paths outside the project root are not rejected here: they are
// reported to the agent, which asks the user about each one.
func resolveTarget(raw, root string) (Target, error) {
	p := strings.TrimSpace(raw)
	var abs string
	switch {
	case p == "" || p == ".":
		abs = root
	case filepath.IsAbs(p):
		abs = filepath.Clean(p)
	default:
		abs = filepath.Clean(filepath.Join(root, p))
	}
	return Target{Raw: raw, Resolved: abs, Outside: !within(root, abs)}, nil
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

// scope is the file handle shared by the file tools: the project's *os.Root
// plus the outside-path approvals the user granted for this one call.
type scope struct {
	root *os.Root
	dir  string // absolute project root
}

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

// display turns a path reported relative to the handle's Root into one the
// model can hand straight back to read_file: project-relative inside the
// project, absolute for an approved outside tree.
func (h handle) display(rel string) string {
	if h.base == "" {
		return rel
	}
	return h.base + "/" + rel
}

// open resolves raw and returns a Root containing it plus the name to use
// inside that Root. In-project paths use the single project Root. An approved
// outside path gets its own Root at the nearest existing directory, so even
// after approval it cannot reach outside that subtree, and symlinks there are
// still checked against it.
func (s scope) open(ctx context.Context, raw string) (handle, error) {
	t, err := resolveTarget(raw, s.dir)
	if err != nil {
		return handle{}, err
	}
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

// nearestExistingDir walks up from a path's directory until it finds one that
// exists, and returns it with the target's name relative to it.
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

// WithApprovals records the outside paths the user agreed to for a single tool
// call. Nothing is remembered between calls: each attempt asks again.
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
