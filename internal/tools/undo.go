package tools

import (
	"fmt"
	"os"
	"path/filepath"
)

// Change is one file as it was before a tool touched it.
type Change struct {
	dir     string // the directory a Root is opened on: the project root, or an outside file's folder
	name    string // the file inside that directory
	shown   string // how the tool named it to the model
	prev    []byte // what it held before
	existed bool   // false means the change created it, so undoing removes it
}

// UndoStack remembers file changes in the order they happened. Writes and edits push onto it;
// /undo takes the top off. It is deliberately small: one file, one snapshot, newest first.
type UndoStack struct {
	changes []Change
}

// push records a file before it changes. A change that cannot be remembered is not undoable,
// and saying so is better than pretending the undo worked.
func (u *UndoStack) push(c Change) {
	if u == nil {
		return
	}
	u.changes = append(u.changes, c)
}

// Empty says there is nothing to put back.
func (u *UndoStack) Empty() bool { return u == nil || len(u.changes) == 0 }

// Undo puts the most recent change back and says what it did.
func (u *UndoStack) Undo() (string, error) {
	if u.Empty() {
		return "", nil
	}
	c := u.changes[len(u.changes)-1]
	root, err := os.OpenRoot(c.dir)
	if err != nil {
		return "", fmt.Errorf("undo %s: %w", c.shown, err)
	}
	defer root.Close()

	if c.existed {
		if err := root.WriteFile(c.name, c.prev, 0o644); err != nil {
			return "", fmt.Errorf("undo %s: %w", c.shown, err)
		}
		u.changes = u.changes[:len(u.changes)-1]
		return fmt.Sprintf("put back %s (%d bytes)", c.shown, len(c.prev)), nil
	}
	if err := root.Remove(c.name); err != nil {
		return "", fmt.Errorf("undo %s: %w", c.shown, err)
	}
	u.changes = u.changes[:len(u.changes)-1]
	return fmt.Sprintf("removed %s, which did not exist before", c.shown), nil
}

// outsideDir splits an absolute path into the directory to open and the file inside it.
func outsideDir(abs string) (dir, name string) {
	dir, name = filepath.Dir(abs), filepath.Base(abs)
	return dir, name
}
