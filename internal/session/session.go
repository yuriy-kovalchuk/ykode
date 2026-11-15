// Package session keeps a run's conversation on disk so another run can pick it up.
// The log is append-only: one JSON line per message, never edited, never trimmed. What a run does to
// the conversation after it was said - shortening an old result, dropping a turn - is a line of its
// own, so reading the file back gives the conversation as the run last sent it, byte for byte. That
// is what a server's prompt cache compares: a resume that words one old message differently makes
// the server read everything after it again.
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
	// A reader that cannot understand a newer format says so rather than rebuilding a conversation it
	// cannot interpret, which is the difference between refusing to resume and resuming wrongly.
}

// format is this program's session format. Lines added to it raise the number.
const format = 1

// record is a message as the log keeps it: the message the server sees, and beside it the thinking
// that came with it, which no request carries but a resumed session should still be able to show.
type record struct {
	llm.Message
	Thinking string `json:"thinking,omitempty"`
	// Sent says the thinking also goes back to the model, so it is kept once, under thinking,
	// instead of twice on every assistant line.
	Sent bool `json:"sent,omitempty"`
}

func recordOf(m llm.Message) record {
	r := record{Message: m, Thinking: m.Reasoning}
	if m.WireReasoning != "" && m.WireReasoning == m.Reasoning {
		r.Message.WireReasoning, r.Sent = "", true
	}
	return r
}

func (r record) message() llm.Message {
	r.Message.Reasoning = r.Thinking
	if r.Sent { // an older file carries both fields and needs neither flag
		r.Message.WireReasoning = r.Thinking
	}
	return r.Message
}

// A boundary line says the conversation before it no longer counts: a cleared one starts again, and
// a folded one continues from the messages the fold kept. The log stays append-only either way: what
// came before a boundary is still in the file, it is only no longer sent. A fold also says what it
// did, so the file can be read on its own: the summary, the files the folded turns touched, and how
// much it took away.
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

// change is a line that alters the conversation written so far without starting it over: one
// message put in another's place, or a run of messages taken out. A position counts the messages
// since the last boundary, from 0. A line of this kind can also carry what the run learned about the
// server, which is no part of the conversation and is needed to carry it on the same way.
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

// Cleared marks the saved conversation as started over: a resume reads what follows the mark, not
// what came before it.
func (l *Log) Cleared() error {
	if err := l.write(mustJSON(boundary{Boundary: boundaryCleared})); err != nil {
		return err
	}
	if l != nil {
		l.messages = 0 // positions for the changes that follow count from here
	}
	return nil
}

// Folded saves what a fold left standing, so a resume starts from the recap rather than reloading
// the turns that were folded away and trimming them again.
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

// Dropped takes a run of messages out of the conversation by position, which is what a trim does: it
// deletes the oldest turns and writes nothing else. Saving the whole conversation for each one appends
// a copy of everything - including the full text of every file read - to a file whose whole design is
// to grow by one line at a time. A position outside what this log is tracking is an error rather than a
// silent no: the caller then writes the conversation whole, which cannot delete the wrong turns.
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

// Revised puts one message in another's place, by position: what a repair does when it replaces a
// call's unreadable arguments with an empty object.
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

// Rewritten saves the conversation whole, as it now stands: for a repair that moved messages about
// rather than changing one.
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

// Learned keeps how many bytes the server counted as one token. A run that comes back without it
// measures the same conversation with a guess, and may trim what the first run had no need to.
func (l *Log) Learned(bytesPerToken float64) error {
	return l.write(mustJSON(change{BytesPerToken: bytesPerToken}))
}

// write appends JSON lines, opening the file with its project header when it is new. The result is
// named so the deferred Close can report a failure the write did not: on a network mount a write the
// filesystem accepted and a close that then failed is a record that never reached the disk.
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

// Load reads a conversation back for the project at root, refusing one another project started.
// A half-written last line (the process was killed mid-write) is dropped rather than failing the resume.
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
				// a newer program wrote this one. Rebuilding it with the wrong reading of its lines gives back
				// a conversation that looks fine and is wrong; saying so gives the user a run that works.
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

// apply makes a change to the conversation read so far. A change that points outside it was written
// by a run whose file lost lines since: it is left out rather than failing the whole resume.
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
