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
)

// normalize rejects nonsense settings and pins the root to an existing absolute directory.
func (c *Config) normalize() error {
	if c.MaxTurns < 1 {
		return fmt.Errorf("--max-turns must be at least 1, got %d", c.MaxTurns)
	}
	if c.MaxTokens < 0 {
		return fmt.Errorf("--max-tokens must be 0 (no cap) or a positive token count, got %d", c.MaxTokens)
	}
	if c.JSON || c.Prompt != "" {
		c.TUI = false // -p and --json write to stdout; a screen would be drawn over it
	}
	if _, known := llm.ThinkingBudgetTokens(c.ThinkingLevel); !known {
		return fmt.Errorf("thinking level %q: want one of %s", c.ThinkingLevel, strings.Join(llm.ThinkingLevels(), ", "))
	}
	if c.CtxSize < 0 {
		return fmt.Errorf("--ctx must be 0 (auto) or a positive token count, got %d", c.CtxSize)
	}
	if c.ReadLines < 1 || c.SearchLimit < 1 {
		return fmt.Errorf("read_lines and search_limit must be at least 1, got %d and %d", c.ReadLines, c.SearchLimit)
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
	c.ResultDir = filepath.Join(home.Dir(), "results", c.Session)
	c.AuditDir = filepath.Join(home.Dir(), "audit")
	return nil
}

// sessionIDPattern guards against a --continue argument that is not an id but a path.
var sessionIDPattern = regexp.MustCompile(`^s[0-9]{8}-[0-9]{6}(\.[0-9]{3})?$`)

// resumeID turns "last" or an id into a conversation that exists on disk.
func resumeID(homeDir, want string) (string, error) {
	if !sessionIDPattern.MatchString(want) && want != "last" {
		return "", fmt.Errorf("--continue %q: not a session id (they look like s20060102-150405)", want)
	}
	ids, err := session.List(filepath.Join(homeDir, "sessions"))
	if err != nil {
		return "", err
	}
	if want == "last" {
		if len(ids) == 0 {
			return "", fmt.Errorf("--continue last: no saved conversation in %s", filepath.Join(homeDir, "sessions"))
		}
		return ids[0], nil
	}
	for _, id := range ids {
		if id == want {
			return id, nil
		}
	}
	return "", fmt.Errorf("--continue %q: no such conversation in %s", want, filepath.Join(homeDir, "sessions"))
}
