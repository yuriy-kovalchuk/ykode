package tools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	// O_EXCL: an id is handed out once, so writing over one would point an older recall at a
	// newer result
	f, err := os.OpenFile(filepath.Join(s.dir, id+".txt"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
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
