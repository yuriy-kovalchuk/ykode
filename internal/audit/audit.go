// Package audit records every tool call the agent makes, in append-only JSONL.
// It is separate from the conversation: it is never trimmed or compacted, it records calls that
// were refused, and it survives /reset.
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
	dir     string // $YKODE_HOME/audit
	session string
}

// New points a logger at a directory; an empty dir means auditing is off.
func New(dir, session string) *Logger {
	if dir == "" {
		return nil
	}
	return &Logger{dir: dir, session: session}
}

// Append writes one entry, with anything that looks like a secret masked first.
func (l *Logger) Append(e Entry) error {
	if l == nil {
		return nil
	}
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

// Read returns the entries of one session, oldest first.
func (l *Logger) Read(session string) ([]Entry, error) {
	if l == nil {
		return nil, nil
	}
	files, err := filepath.Glob(filepath.Join(l.dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("audit: %w", err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			if line == "" {
				continue
			}
			var e Entry
			if err := json.Unmarshal([]byte(line), &e); err != nil {
				return nil, fmt.Errorf("audit: %s: %w", filepath.Base(path), err)
			}
			if e.Session == session {
				entries = append(entries, e)
			}
		}
	}
	return entries, nil
}

// secretRe marks the parts of a command or diff that must never reach the log in clear text.
var secretRe = regexp.MustCompile(`(?i)((?:[a-z0-9_]*(?:key|token|secret|password)|authorization)\s*[:=]\s*)(\S+)`)

const masked = "[masked]"

func redact(s string) string {
	return secretRe.ReplaceAllString(s, "$1"+masked)
}

func redactAll(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = redact(v)
	}
	return out
}
