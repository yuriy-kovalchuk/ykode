package tools

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"slices"
	"sort"
	"strings"
)

// stopWalk ends a walk early without an error, used once enough results are in.
var stopWalk = errors.New("walk: enough results")

const (
	maxScannedFileBytes = 1 << 20 // search and find skip files larger than 1 MB
	skipDirName         = ".git"
)

// walkFiles walks the tree under start inside root, honouring every .gitignore
// it descends through, and calls visit for each text file, with its contents.
// Symlinked directories are not followed (the Root refuses symlinks that escape
// anyway). visit may return stopWalk to finish early.
func walkFiles(ctx context.Context, root *os.Root, start string, visit func(path string, data []byte) error) error {
	return walkTree(ctx, root, start, visit, true)
}

// walkNames is walkFiles without reading anything: listing names must see
// binary and oversized files too, exactly as fd does.
func walkNames(ctx context.Context, root *os.Root, start string, visit func(path string, data []byte) error) error {
	return walkTree(ctx, root, start, visit, false)
}

func walkTree(ctx context.Context, root *os.Root, start string, visit func(path string, data []byte) error, read bool) error {
	w := walker{root: root, visit: visit}
	err := w.walk(ctx, filepathCleanSlash(start), nil, read)
	if errors.Is(err, stopWalk) {
		return nil
	}
	return err
}

func filepathCleanSlash(p string) string {
	if p == "" || p == "." {
		return "."
	}
	return strings.TrimSuffix(p, "/")
}

// joinSlash builds a Root-relative path; os.Root rejects "./" prefixes, which is
// why the walk root is "." rather than "./".
func joinSlash(dir, name string) string {
	if dir == "." {
		return name
	}
	return dir + "/" + name
}

type walker struct {
	root  *os.Root
	visit func(path string, data []byte) error
}

func (w walker) walk(ctx context.Context, dir string, ignores ignoreSet, read bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := w.root.Open(dir)
	if err != nil {
		return nil // an unreadable directory is simply not searched
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	if ig := w.readIgnore(dir, entries); ig != nil {
		ignores = slices.Concat(ignores, ignoreSet{ig})
	}
	var dirs []string
	for _, e := range entries {
		if e.Name() == skipDirName || e.Name() == ".gitignore" {
			continue
		}
		path := joinSlash(dir, e.Name())
		if e.IsDir() {
			if !ignored(path, ignores, true) {
				dirs = append(dirs, path)
			}
			continue
		}
		if e.Type()&fs.ModeSymlink != 0 || ignored(path, ignores, false) {
			continue // never follow symlinks while searching
		}
		if info, err := e.Info(); err != nil || (read && info.Size() > maxScannedFileBytes) {
			continue
		}
		var data []byte
		if read {
			var err error
			data, err = w.root.ReadFile(path)
			if err != nil || !isText(data) {
				continue
			}
		}
		if err := w.visit(path, data); err != nil {
			return err
		}
	}
	for _, d := range dirs {
		if err := w.walk(ctx, d, ignores, read); err != nil {
			return err
		}
	}
	return nil
}

func (w walker) readIgnore(dir string, entries []fs.DirEntry) *ignoreFile {
	var e fs.DirEntry
	for _, entry := range entries {
		if entry.Name() == ".gitignore" {
			e = entry
			break
		}
	}
	if e == nil || e.IsDir() {
		return nil
	}
	data, err := w.root.ReadFile(joinSlash(dir, ".gitignore"))
	if err != nil {
		return nil
	}
	return &ignoreFile{dir: dir, rules: parseIgnoreRules(string(data))}
}

// isText looks at the first 8 KB, which is where a binary hides its NUL bytes.
func isText(data []byte) bool {
	for _, b := range data[:min(len(data), 8192)] {
		if b == 0 {
			return false
		}
	}
	return true
}
