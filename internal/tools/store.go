package tools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// resultIDPattern keeps a recall id from turning into a path.
var resultIDPattern = regexp.MustCompile(`^r[0-9]{1,6}$`)

// ResultStore keeps tool output the conversation dropped, so recall can hand it back.
type ResultStore struct {
	dir string
}

// NewResultStore points a store at a directory; an empty dir means output is dropped, not saved.
func NewResultStore(dir string) ResultStore { return ResultStore{dir: dir} }

// Saved says whether output is kept anywhere.
func (s ResultStore) Saved() bool { return s.dir != "" }

// Save writes one result under its id.
func (s ResultStore) Save(id, text string) error {
	if !resultIDPattern.MatchString(id) {
		return fmt.Errorf("store: bad result id %q", id)
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	if err := os.Chmod(s.dir, 0o700); err != nil { // a directory made before this was readable by the machine
		return fmt.Errorf("store: %w", err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, id+".txt"), []byte(text), 0o600); err != nil {
		return fmt.Errorf("store: %w", err)
	}
	return nil
}

// Load reads back one saved result.
func (s ResultStore) Load(id string) (string, error) {
	if !resultIDPattern.MatchString(id) {
		return "", fmt.Errorf("bad result id %q: ids look like r7", id)
	}
	data, err := os.ReadFile(filepath.Join(s.dir, id+".txt"))
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("nothing is saved for %s: it was never shortened, or it aged out", id)
	}
	if err != nil {
		return "", fmt.Errorf("read saved result: %w", err)
	}
	return string(data), nil
}

// PruneOldDirs deletes subdirectories of dir older than maxAge, and reports how many went.
func PruneOldDirs(dir, prefix string, maxAge time.Duration) (int, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil // a first run has nothing to clean
	}
	if err != nil {
		return 0, fmt.Errorf("clean old files in %s: %w", dir, err)
	}
	removed := 0
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return removed, fmt.Errorf("stat %s: %w", e.Name(), err)
		}
		if time.Since(info.ModTime()) < maxAge {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return removed, fmt.Errorf("remove %s: %w", e.Name(), err)
		}
		removed++
	}
	return removed, nil
}
