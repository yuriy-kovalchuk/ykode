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
	path string
}

// New points a log at <dir>/<id>.jsonl; an empty id means sessions are off.
func New(dir, id string) *Log {
	if dir == "" || id == "" {
		return nil
	}
	return &Log{path: filepath.Join(dir, id+".jsonl")}
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
	if err := os.MkdirAll(filepath.Dir(l.path), dirPerm); err != nil {
		return fmt.Errorf("session: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePerm)
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	defer f.Close()
	raw, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	if _, err := f.Write(append(raw, '\n')); err != nil {
		return fmt.Errorf("session: %w", err)
	}
	return nil
}

// Load reads a conversation back. A half-written last line (the process was killed mid-write)
// is dropped rather than failing the whole resume.
func Load(path string) ([]llm.Message, error) {
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
