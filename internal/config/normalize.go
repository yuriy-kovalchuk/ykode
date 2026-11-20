package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/session"
	"strings"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/home"
	"github.com/yuriy-kovalchuk/ykode/internal/theme"
)

// normalize rejects nonsense settings and pins the root to an existing absolute directory.
func (c *Config) normalize() error {
	if c.MaxTurns < 0 {
		return fmt.Errorf("--max-turns must be 0 (no limit) or a positive count, got %d", c.MaxTurns)
	}
	if c.MaxTokens < 0 {
		return fmt.Errorf("--max-tokens must be 0 (no cap) or a positive token count, got %d", c.MaxTokens)
	}
	if c.Prompt != "" {
		c.TUI = false // -p writes the answer to stdout; a screen would be drawn over it
	}
	if _, known := llm.ThinkingBudgetTokens(c.ThinkingLevel); !known {
		return fmt.Errorf("thinking level %q: want one of %s", c.ThinkingLevel, strings.Join(llm.ThinkingLevels(), ", "))
	}
	if err := theme.Check(c.Theme); err != nil {
		return err
	}
	if c.CtxSize < 0 {
		return fmt.Errorf("--ctx must be 0 (auto) or a positive token count, got %d", c.CtxSize)
	}
	if c.ReadLines < 1 {
		return fmt.Errorf("read_lines must be at least 1, got %d", c.ReadLines)
	}
	root, err := filepath.Abs(c.Root)
	if err != nil {
		return fmt.Errorf("--root %q: %w", root, err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("--root %q: %w", root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("--root %q: not a directory", root)
	}
	c.Root = root
	c.BaseURL = strings.TrimSuffix(c.BaseURL, "/")
	if c.CommandLogDir == "" {
		c.CommandLogDir = filepath.Join(home.Dir(), "logs")
	}
	c.Session = "s" + time.Now().Format(sessionStamp)
	if c.Continue != "" {
		id, err := resumeID(home.Dir(), c.Continue)
		if err != nil {
			return err
		}
		c.Session = id // the resumed run keeps writing to the same conversation, results and log
	}
	c.SessionDir = filepath.Join(home.Dir(), "sessions")
	c.SessionFile = filepath.Join(c.SessionDir, c.Session+".jsonl")
	return nil
}

// sessionIDPattern guards against a --continue argument that is not an id but a path.
var sessionIDPattern = regexp.MustCompile(`^s[0-9]{8}-[0-9]{6}(\.[0-9]{3})?$`)

// minIDPart is how much of an id must be typed to count as one: an id is a timestamp to the millisecond
const minIDPart = 3

// resumeID turns "last", an id, or enough of an id into a conversation that exists on disk.
func resumeID(homeDir, want string) (string, error) {
	if want != "last" && !sessionIDPattern.MatchString(want) && !partOfAnID(want) {
		return "", fmt.Errorf("--continue %q: not a session id (they look like s20060102-150405, and enough of one works)", want)
	}
	dir := filepath.Join(homeDir, "sessions")
	ids, err := session.List(dir)
	if err != nil {
		return "", err
	}
	if want == "last" {
		if len(ids) == 0 {
			return "", fmt.Errorf("--continue last: no saved conversation in %s", dir)
		}
		return ids[0], nil
	}
	if sessionIDPattern.MatchString(want) {
		for _, id := range ids {
			if id == want {
				return id, nil
			}
		}
		return "", fmt.Errorf("--continue %q: no such conversation in %s", want, dir)
	}
	var matches []string
	for _, id := range ids {
		if strings.Contains(id, want) {
			matches = append(matches, id)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("--continue %q: no such conversation in %s", want, dir)
	default:
		return "", fmt.Errorf("--continue %q could be %d conversations: %s", want, len(matches), strings.Join(matches, ", "))
	}
}

// partOfAnID says a --continue argument holds part of an id: digits, and the dash between date and time
func partOfAnID(want string) bool {
	digits := 0
	for _, r := range want {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '-':
		default:
			return false
		}
	}
	return digits >= minIDPart
}
