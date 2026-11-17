// Package session keeps a conversation on disk as an append-only log, so a resume sends the same bytes again
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
	Root   string `json:"root"`
	Format int    `json:"format,omitempty"` // how the rest of the file is written; 0 is a file from before this field
	// a reader that cannot understand a newer format refuses, rather than resuming a conversation wrongly
}

// format is this program's session format; lines that change how a line reads raise the number
const format = 2

// record is a message as the log keeps it, with the thinking that came with it beside it.
type record struct {
	llm.Message
	Thinking string `json:"thinking,omitempty"`
}

// recordOf keeps the thinking once, under thinking, rather than also as the wire's reasoning_content.
func recordOf(m llm.Message) record {
	r := record{Message: m, Thinking: m.Reasoning}
	r.Message.Reasoning = ""
	return r
}

func (r record) message() llm.Message {
	if r.Thinking != "" {
		r.Message.Reasoning = r.Thinking
	}
	return r.Message
}

// a boundary line says the conversation before it no longer counts, while the log stays append-only
type boundary struct {
	Boundary      string   `json:"boundary"`
	Kept          []record `json:"kept,omitempty"`
	Summary       string   `json:"summary,omitempty"`
	ReadFiles     []string `json:"read_files,omitempty"`
	ModifiedFiles []string `json:"modified_files,omitempty"`
	Dropped       int      `json:"dropped,omitempty"`       // messages the fold took out of the conversation
	TokensBefore  int      `json:"tokens_before,omitempty"` // what the conversation cost before it
}

// Fold is what a compaction did, for the log to keep beside the messages that survived it.
type Fold struct {
	Kept          []llm.Message
	Summary       string
	ReadFiles     []string
	ModifiedFiles []string
	Dropped       int
	TokensBefore  int
}

const (
	boundaryCleared   = "cleared"
	boundaryFolded    = "folded"
	boundaryRewritten = "rewritten"
)

// change alters the conversation so far without starting it over; a position counts from the last boundary
type change struct {
	Revised       *int    `json:"revised,omitempty"`
	Message       *record `json:"message,omitempty"`
	Removed       []int   `json:"removed,omitempty"`         // [from, to): the messages taken out
	BytesPerToken float64 `json:"bytes_per_token,omitempty"` // how the server counted the last request
}

// Saved is a conversation as a log holds it, and what the run that wrote it had learned.
type Saved struct {
	Messages      []llm.Message
	BytesPerToken float64 // 0 when the run never heard a count from its server
}

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
	raw, err := json.Marshal(recordOf(m))
	if err != nil {
		return fmt.Errorf("session: %w", err)
	}
	if err := l.write(raw); err != nil {
		return err
	}
	l.messages++
	return nil
}

// Cleared marks the conversation as started over: a resume reads what follows the mark
func (l *Log) Cleared() error {
	if err := l.write(mustJSON(boundary{Boundary: boundaryCleared})); err != nil {
		return err
	}
	if l != nil {
		l.messages = 0 // positions for the changes that follow count from here
	}
	return nil
}

// Folded saves what a fold left standing, so a resume starts from the recap
func (l *Log) Folded(f Fold) error {
	kept := make([]record, len(f.Kept))
	for i, m := range f.Kept {
		kept[i] = recordOf(m)
	}
	if err := l.write(mustJSON(boundary{Boundary: boundaryFolded, Kept: kept, Summary: f.Summary,
		ReadFiles: f.ReadFiles, ModifiedFiles: f.ModifiedFiles, Dropped: f.Dropped, TokensBefore: f.TokensBefore})); err != nil {
		return err
	}
	if l != nil {
		l.messages = len(f.Kept) // a boundary restarts the positions the change lines point at
	}
	return nil
}

// Dropped removes a run of messages by position, which is what a trim does in one appended line
func (l *Log) Dropped(from, to int) error {
	if l == nil {
		return nil
	}
	if from < 0 || to <= from || to > l.messages {
		return fmt.Errorf("session: position %d..%d is outside the %d messages saved", from, to, l.messages)
	}
	if err := l.write(mustJSON(change{Removed: []int{from, to}})); err != nil {
		return err
	}
	l.messages -= to - from
	return nil
}

// Revised puts one message in another's place by position: what a repair does to a bad call
func (l *Log) Revised(at int, m llm.Message) error {
	if l == nil {
		return nil
	}
	if at < 0 || at >= l.messages {
		return fmt.Errorf("session: position %d is outside the %d messages saved", at, l.messages)
	}
	r := recordOf(m)
	return l.write(mustJSON(change{Revised: &at, Message: &r}))
}

// Rewritten saves the conversation whole, for a repair that moved messages about
func (l *Log) Rewritten(kept []llm.Message) error {
	records := make([]record, len(kept))
	for i, m := range kept {
		records[i] = recordOf(m)
	}
	if err := l.write(mustJSON(boundary{Boundary: boundaryRewritten, Kept: records})); err != nil {
		return err
	}
	if l != nil {
		l.messages = len(kept)
	}
	return nil
}

// Learned keeps the bytes the server counted as one token, so the next run measures with it
func (l *Log) Learned(bytesPerToken float64) error {
	return l.write(mustJSON(change{BytesPerToken: bytesPerToken}))
}

// write appends JSON lines, naming the result so a deferred Close can report what the write did not
func (l *Log) write(lines ...[]byte) (err error) {
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
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("session: close %s: %w", l.path, cerr)
		}
	}()
	var out []byte
	if info, statErr := f.Stat(); statErr == nil && info.Size() == 0 && l.root != "" {
		head, _ := json.Marshal(header{Root: l.root, Format: format})
		out = append(out, append(head, '\n')...)
	}
	for _, line := range lines {
		out = append(out, append(line, '\n')...)
	}
	if _, werr := f.Write(out); werr != nil {
		return fmt.Errorf("session: %w", werr)
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

// Load reads a conversation for the project at root, refusing one another project started
func Load(path, root string) ([]llm.Message, error) {
	saved, err := LoadAll(path, root)
	return saved.Messages, err
}

// LoadAll is Load with what the run had learned beside the conversation.
func LoadAll(path, root string) (Saved, error) {
	var saved Saved
	messages, err := load(path, root, &saved)
	saved.Messages = messages
	return saved, err
}

func load(path, root string, saved *Saved) ([]llm.Message, error) {
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
			if h.Format > format {
				// a newer program wrote this: rebuilding it looks fine and is wrong, so refuse and say so
				return nil, fmt.Errorf("session: %s was written by a newer ykode (format %d); this one reads %d", filepath.Base(path), h.Format, format)
			}
			continue
		}
		var b boundary
		if json.Unmarshal([]byte(line), &b) == nil && b.Boundary != "" {
			messages = messages[:0] // what came before the mark no longer counts
			for _, r := range b.Kept {
				messages = append(messages, r.message())
			}
			continue
		}
		var c change
		if json.Unmarshal([]byte(line), &c) == nil && (c.Revised != nil || c.Removed != nil || c.BytesPerToken > 0) {
			messages = c.apply(messages)
			if c.BytesPerToken > 0 {
				saved.BytesPerToken = c.BytesPerToken
			}
			continue
		}
		var r record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			if i == len(lines)-1 {
				break // the tail was cut short: keep what is whole
			}
			return nil, fmt.Errorf("session: %s line %d: %w", filepath.Base(path), i+1, err)
		}
		messages = append(messages, r.message())
	}
	return messages, nil
}

// apply skips a change pointing outside the conversation, rather than failing the whole resume
func (c change) apply(messages []llm.Message) []llm.Message {
	if c.Revised != nil && c.Message != nil && *c.Revised >= 0 && *c.Revised < len(messages) {
		messages[*c.Revised] = c.Message.message()
	}
	if len(c.Removed) == 2 && c.Removed[0] >= 0 && c.Removed[0] <= c.Removed[1] && c.Removed[1] <= len(messages) {
		messages = append(messages[:c.Removed[0]], messages[c.Removed[1]:]...)
	}
	return messages
}

// ID is the id a saved conversation's file is named for: <id>.jsonl.
func ID(path string) string {
	if path == "" {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(path), ".jsonl")
}

// List names the saved conversations newest first: the ids are stamped with the run time
func List(dir string) ([]string, error) {
	names, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	ids := make([]string, len(names))
	for i, name := range names {
		ids[i] = ID(name)
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
