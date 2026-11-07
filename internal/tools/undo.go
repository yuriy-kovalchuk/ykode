package tools

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

// Change is one file as it was before a tool touched it.
type Change struct {
	dir     string   // the directory a Root is opened on: the project root, or an outside file's folder
	name    string   // the file inside that directory
	shown   string   // how the tool named it to the model
	prev    []byte   // what it held before
	wrote   [32]byte // sha256 of what the tool left there, so a later hand edit is not undone blindly
	existed bool     // false means the change created it, so undoing removes it
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

// Undo puts the newest change back; unless forced, not over an edit made since the tool's.
func (u *UndoStack) Undo(force bool) (string, error) {
	if u.Empty() {
		return "", nil
	}
	c := u.changes[len(u.changes)-1]
	root, err := os.OpenRoot(c.dir)
	if err != nil {
		return "", fmt.Errorf("undo %s: %w", c.shown, err)
	}
	defer root.Close()

	now, _ := root.ReadFile(c.name)
	if sum := sha256.Sum256(now); !force && !bytes.Equal(sum[:], c.wrote[:]) {
		return "", fmt.Errorf("undo %s: the file changed after ykode wrote it; /undo force puts the old text back anyway", c.shown)
	}
	if c.existed {
		err = root.WriteFile(c.name, c.prev, 0o644)
	} else {
		err = root.Remove(c.name)
	}
	if err != nil {
		return "", fmt.Errorf("undo %s: %w", c.shown, err)
	}
	u.changes = u.changes[:len(u.changes)-1]
	if !c.existed {
		return fmt.Sprintf("removed %s, which did not exist before", c.shown), nil
	}
	return fmt.Sprintf("put back %s (%d bytes)", c.shown, len(c.prev)), nil
}

// outsideDir splits an absolute path into the directory to open and the file inside it.
func outsideDir(abs string) (dir, name string) {
	dir, name = filepath.Dir(abs), filepath.Base(abs)
	return dir, name
}
