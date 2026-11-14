// Package audit records every tool call the agent makes, in append-only JSONL.
// It is separate from the conversation: it is never trimmed or compacted, it records calls that
// were refused, and it survives /reset.
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const dirPerm = 0o700
const filePerm = 0o600

// Entry is one line of the log. A call writes a "started" line before it runs and a final line after.
type Entry struct {
	Time        time.Time `json:"time"`
	DurationMS  int64     `json:"duration_ms,omitempty"`
	ID          string    `json:"id"`
	Session     string    `json:"session"`
	Turn        int       `json:"turn"`
	Root        string    `json:"root"`
	Model       string    `json:"model"`
	Tool        string    `json:"tool"`
	Args        string    `json:"args"`
	Paths       []string  `json:"paths,omitempty"`
	OutsideRoot []string  `json:"outside_root,omitempty"`
	Decision    string    `json:"decision,omitempty"` // auto, yes_flag, allowlist, user_yes, user_no
	Status      string    `json:"status"`             // started, ok, error, refused, cancelled, timeout
	ExitCode    *int      `json:"exit_code,omitempty"`
	ResultBytes int       `json:"result_bytes,omitempty"`
	ResultSHA   string    `json:"result_sha256,omitempty"`
	OutputPath  string    `json:"output_path,omitempty"`
	Diff        string    `json:"diff,omitempty"`
}

// Logger appends entries for one run into one monthly file.
type Logger struct {
	mu      sync.Mutex // overlapping tool calls write one line each, never half a line
	dir     string     // $YKODE_HOME/audit
	session string
}

// New points a logger at a directory; an empty dir means auditing is off.
func New(dir, session string) *Logger {
	if dir == "" {
		return nil
	}
	return &Logger{dir: dir, session: session}
}

// ContinueAs says the run came back to another saved conversation: the entries from now on belong to
// that one, so a look back at either finds its own calls.
func (l *Logger) ContinueAs(session string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.session = session
}

// Append writes one entry, with anything that looks like a secret masked first.
func (l *Logger) Append(e Entry) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e.Session = l.session
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	e.Args, e.Diff = redact(e.Args), redact(e.Diff)
	e.Paths = redactAll(e.Paths)

	if err := os.MkdirAll(l.dir, dirPerm); err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	path := filepath.Join(l.dir, e.Time.Format("2006-01")+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, filePerm)
	if err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	defer f.Close()
	raw, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	if _, err := f.Write(append(raw, '\n')); err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	return nil
}

// Read returns the entries of one session, oldest first, and how many log lines could not be
// read; an empty session matches every run.
func (l *Logger) Read(session string) ([]Entry, int, error) {
	return l.matching(startMonth(session), func(e Entry) bool { return session == "" || e.Session == session })
}

// startMonth names the month file a session began in (s20261107-… → 2026-11), "" when unknown.
func startMonth(session string) string {
	if len(session) < 7 || session[0] != 's' {
		return ""
	}
	return session[1:5] + "-" + session[5:7]
}

// ReadRoot returns every entry logged for one project root, across sessions.
func (l *Logger) ReadRoot(root string) ([]Entry, int, error) {
	return l.matching("", func(e Entry) bool { return e.Root == root })
}

// matching reads the log file by file - name order is month order - and keeps what the filter takes.
func (l *Logger) matching(since string, keep func(Entry) bool) ([]Entry, int, error) {
	if l == nil {
		return nil, 0, nil
	}
	files, err := filepath.Glob(filepath.Join(l.dir, "*.jsonl"))
	if err != nil {
		return nil, 0, err
	}
	var entries []Entry
	damaged := 0
	for _, path := range files {
		if filepath.Base(path) < since { // a month before the run began cannot hold its calls
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, 0, fmt.Errorf("audit: %w", err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			if line == "" {
				continue
			}
			var e Entry
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				// a torn line is one call lost, not the whole month lost: the log is append-only
				// and somebody may have edited it, so keep reading and say how much was missing.
				damaged++
				continue
			}
			if keep(e) {
				entries = append(entries, e)
			}
		}
	}
	return entries, damaged, nil
}

// Session names the run this logger writes to.
func (l *Logger) Session() string {
	if l == nil {
		return ""
	}
	return l.session
}

// LatestSession names the most recent run in the log, "" when there is none.
func (l *Logger) LatestSession() string {
	if l == nil {
		return ""
	}
	files, _ := filepath.Glob(filepath.Join(l.dir, "*.jsonl"))
	if len(files) == 0 {
		return ""
	}
	entries, _, err := l.matching(filepath.Base(files[len(files)-1]), func(Entry) bool { return true })
	if err != nil || len(entries) == 0 {
		return ""
	}
	// the files are read in name order, which is month order, so the last line is the newest run
	return entries[len(entries)-1].Session
}

// ReadAll returns every entry, oldest first.
func (l *Logger) ReadAll() ([]Entry, int, error) { return l.Read("") }
