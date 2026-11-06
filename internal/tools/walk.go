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

// walkSpec says what one walk reports.
type walkSpec struct {
	start    string
	read     bool // pass file contents to visit
	dirs     bool // report directories as well, with a trailing slash
	maxDepth int  // 1 reports only the entries of start; 0 has no limit
}

// walkFiles walks start honouring .gitignore files, calling visit per text file with its contents.
func walkFiles(ctx context.Context, root *os.Root, start string, visit func(path string, data []byte) error) error {
	return walkTree(ctx, root, walkSpec{start: start, read: true}, visit)
}

// walkNames is walkFiles without reading: listing sees binary and oversized files too, as fd does.
func walkNames(ctx context.Context, root *os.Root, start string, visit func(path string, data []byte) error) error {
	return walkTree(ctx, root, walkSpec{start: start}, visit)
}

// walkListing reports files and directories to a depth, which is how a directory gets listed.
func walkListing(ctx context.Context, root *os.Root, start string, depth int, visit func(path string, data []byte) error) error {
	return walkTree(ctx, root, walkSpec{start: start, dirs: true, maxDepth: depth}, visit)
}

func walkTree(ctx context.Context, root *os.Root, spec walkSpec, visit func(path string, data []byte) error) error {
	w := walker{root: root, visit: visit, spec: spec}
	err := w.walk(ctx, filepathCleanSlash(spec.start), nil, 1)
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

// joinSlash builds a Root-relative path; os.Root rejects "./" prefixes, so the walk root is ".".
func joinSlash(dir, name string) string {
	if dir == "." {
		return name
	}
	return dir + "/" + name
}

type walker struct {
	root  *os.Root
	visit func(path string, data []byte) error
	spec  walkSpec
}

func (w walker) walk(ctx context.Context, dir string, ignores ignoreSet, depth int) error {
	read := w.spec.read
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
			if ignored(path, ignores, true) {
				continue
			}
			if w.spec.dirs {
				if err := w.visit(path+"/", nil); err != nil {
					return err
				}
			}
			if w.spec.maxDepth == 0 || depth < w.spec.maxDepth {
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
		if err := w.walk(ctx, d, ignores, depth+1); err != nil {
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
