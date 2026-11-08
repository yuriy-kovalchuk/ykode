// Package session keeps a run's conversation on disk so another run can pick it up.
// The log is append-only: one JSON line per message, never edited, never trimmed.
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

const dirPerm = 0o700
const filePerm = 0o600

// Log writes one conversation.
type Log struct {
	path     string
	root     string // the project, written as the first line so another project cannot resume it
	messages int    // what this run saved, so leaving can say what is there to come back to
}

// header is the first line of a conversation that knows its project.
type header struct {
	Root string `json:"root"`
}

// A boundary line says the conversation before it no longer counts: a cleared one starts again, and
// a folded one continues from the messages the fold kept. The log stays append-only either way.
type boundary struct {
	Boundary string        `json:"boundary"`
	Kept     []llm.Message `json:"kept,omitempty"`
}

const (
	boundaryCleared = "cleared"
	boundaryFolded  = "folded"
)

// New points a log at <dir>/<id>.jsonl for the project at root; an empty id means sessions are off.
func New(dir, id, root string) *Log {
	if dir == "" || id == "" {
		return nil
	}
	return &Log{path: filepath.Join(dir, id+".jsonl"), root: root}
}

// Path names the file this log writes to.
func (l *Log) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Append adds one message to the end of the conversation.
func (l *Log) Append(m llm.Message) error {
	if l == nil {
		return nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	if err := l.write(raw); err != nil {
		return err
	}
	l.messages++
	return nil
}

// Cleared marks the saved conversation as started over: a resume reads what follows the mark, not
// what came before it.
func (l *Log) Cleared() error {
	return l.write(mustJSON(boundary{Boundary: boundaryCleared}))
}

// Folded saves what a fold left standing, so a resume starts from the recap rather than reloading
// the turns that were folded away and trimming them again.
func (l *Log) Folded(kept []llm.Message) error {
	return l.write(mustJSON(boundary{Boundary: boundaryFolded, Kept: kept}))
}

// write appends JSON lines, opening the file with its project header when it is new.
func (l *Log) write(lines ...[]byte) error {
	if l == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(l.path), dirPerm); err != nil {
		return fmt.Errorf("session: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePerm)
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	defer f.Close()
	var out []byte
	if info, err := f.Stat(); err == nil && info.Size() == 0 && l.root != "" {
		head, _ := json.Marshal(header{Root: l.root})
		out = append(out, append(head, '\n')...)
	}
	for _, line := range lines {
		out = append(out, append(line, '\n')...)
	}
	if _, err := f.Write(out); err != nil {
		return fmt.Errorf("session: %w", err)
	}
	return nil
}

// Count is how many messages this run saved. A run that never answered has nothing to resume.
func (l *Log) Count() int {
	if l == nil {
		return 0
	}
	return l.messages
}

// Load reads a conversation back for the project at root, refusing one another project started.
// A half-written last line (the process was killed mid-write) is dropped rather than failing the resume.
func Load(path, root string) ([]llm.Message, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("session: %w", err)
	}
	lines := strings.Split(string(data), "\n")
	var messages []llm.Message
	for i, line := range lines {
		if line == "" {
			continue
		}
		var h header
		if i == 0 && json.Unmarshal([]byte(line), &h) == nil && h.Root != "" {
			if root != "" && h.Root != root {
				return nil, fmt.Errorf("session: %s belongs to %s; resume it with --root %s", filepath.Base(path), h.Root, h.Root)
			}
			continue
		}
		var b boundary
		if json.Unmarshal([]byte(line), &b) == nil && b.Boundary != "" {
			messages = b.Kept // what came before the mark no longer counts
			continue
		}
		var m llm.Message
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			if i == len(lines)-1 {
				break // the tail was cut short: keep what is whole
			}
			return nil, fmt.Errorf("session: %s line %d: %w", filepath.Base(path), i+1, err)
		}
		messages = append(messages, m)
	}
	return messages, nil
}

// List names the saved conversations, newest first. The ids are stamped with the run time,
// so name order is time order.
func List(dir string) ([]string, error) {
	names, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	ids := make([]string, len(names))
	for i, name := range names {
		ids[i] = strings.TrimSuffix(filepath.Base(name), ".jsonl")
	}
	return ids, nil
}

// mustJSON renders a line the log writes; these types are internal, so it cannot fail.
func mustJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		return []byte(`{}`)
	}
	return raw
}
