package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/home"
)

// fileSettings mirrors settings.json; a non-nil pointer means the file set that key.
type fileSettings struct {
	BaseURL      *string `json:"base_url,omitempty"`
	APIKey       *string `json:"api_key,omitempty"`
	APIKeyEnv    *string `json:"api_key_env,omitempty"` // name of the variable that holds it, which beats the key
	Audit        *bool   `json:"audit,omitempty"`
	Retain       *int    `json:"audit_retention_days,omitempty"`
	KeepSessions *string `json:"keep_sessions,omitempty"`
	Model        *string `json:"model,omitempty"`
	MaxTurns     *int    `json:"max_turns,omitempty"`
	MaxTokens    *int    `json:"max_tokens,omitempty"`
	Thinking     *bool   `json:"thinking,omitempty"`
	AssumeYes    *bool   `json:"assume_yes,omitempty"`
	Verify       *bool   `json:"verify,omitempty"`
	Context      struct {
		ReserveOutput *int `json:"reserve_output,omitempty"`
		SafetyMargin  *int `json:"safety_margin,omitempty"`
	} `json:"context,omitempty"`
	Tools struct {
		ReadLines       *int    `json:"read_lines,omitempty"`
		SearchLimit     *int    `json:"search_limit,omitempty"`
		ExecTimeoutSec  *int    `json:"exec_timeout_sec,omitempty"`
		CommandLogDir   *string `json:"command_log_dir,omitempty"`
		KeepCommandLogs *string `json:"keep_command_logs,omitempty"`
	} `json:"tools,omitempty"`
	Permissions struct {
		Allow []string `json:"allow,omitempty"`
	} `json:"permissions,omitempty"`
	Models map[string]struct {
		SendReasoning *bool `json:"send_reasoning,omitempty"`
	} `json:"models,omitempty"`
}

// builtinSettings are the defaults ykode ships with, in the shape of the file, so the
// settings.json a first run writes cannot disagree with what the code would have done.
func builtinSettings() fileSettings {
	d := defaults() // the same list the code resolves from, so the file cannot contradict it
	var s fileSettings
	s.BaseURL, s.Model, s.MaxTurns, s.MaxTokens = p(d.BaseURL), p("default"), p(d.MaxTurns), p(d.MaxTokens)
	s.Thinking, s.Verify, s.AssumeYes = p(d.Think), p(!d.NoVerify), p(d.AssumeYes)
	s.Audit, s.Retain = p(d.LogAudit), p(int(d.KeepAudit/(24*time.Hour)))
	s.Context.SafetyMargin = p(d.SafetyMargin)
	s.Tools.ReadLines, s.Tools.SearchLimit = p(d.ReadLines), p(d.SearchLimit)
	s.KeepSessions = p(hours(d.KeepSessions))
	s.Tools.ExecTimeoutSec = p(int(d.ExecTimeout / time.Second))
	s.Tools.CommandLogDir, s.Tools.KeepCommandLogs = p(""), p(hours(d.KeepCommandLogs))
	return s
}

// hours writes a duration the way the settings file spells it: 24h, not 24h0m0s.
func hours(d time.Duration) string {
	if h := int(d / time.Hour); time.Duration(h)*time.Hour == d {
		return fmt.Sprintf("%dh", h)
	}
	return d.String()
}

// p keeps a default addressable: a non-nil pointer is what tells the layers "this was set".
func p[T any](v T) *T { return &v }

// SeedSettings writes the defaults to $YKODE_HOME/settings.json when nothing is there yet, so
// the file a user might edit exists and shows every key. An existing file is never touched.
func SeedSettings() error {
	path := home.Settings()
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	raw, err := json.MarshalIndent(builtinSettings(), "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// readSettings layers ~/.ykode/settings.json under the project's own settings file.
func (c *Config) readSettings() error {
	if err := c.applyFile(home.Settings(), false); err != nil {
		return err
	}
	return c.applyFile(projectSettingsPath(c.Root), true)
}

// applyFile reads one settings file and layers it over what came before. A project's file is a
// stranger: it may say how the project works, and everything else waits until the user trusts it.
func (c *Config) applyFile(path string, project bool) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var s fileSettings
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if project {
		c.ProjectSettings = path
		if !acceptedProject(c.Root, raw) {
			c.HeldKeys = stripUnsafe(&s)
		}
	}
	return c.applySettings(s, path)
}

// stripUnsafe empties every key a project may not set on its own and names what it took. What is
// left is how the project is read and what a turn costs; what is taken is where the run sends
// what it finds, who holds a key, and who answers the asking - and a directory you cloned does not
// get to decide those.
func stripUnsafe(s *fileSettings) []string {
	var held []string
	drop := func(name string, isSet bool, clear func()) {
		if isSet {
			held = append(held, name)
			clear()
		}
	}
	drop("base_url", s.BaseURL != nil, func() { s.BaseURL = nil })
	drop("api_key", s.APIKey != nil, func() { s.APIKey = nil })
	drop("api_key_env", s.APIKeyEnv != nil, func() { s.APIKeyEnv = nil })
	drop("model", s.Model != nil, func() { s.Model = nil })
	drop("assume_yes", s.AssumeYes != nil, func() { s.AssumeYes = nil })
	drop("audit", s.Audit != nil, func() { s.Audit = nil })
	drop("audit_retention_days", s.Retain != nil, func() { s.Retain = nil })
	drop("permissions.allow", s.Permissions.Allow != nil, func() { s.Permissions.Allow = nil })
	drop("tools.command_log_dir", s.Tools.CommandLogDir != nil, func() { s.Tools.CommandLogDir = nil })
	drop("tools.keep_command_logs", s.Tools.KeepCommandLogs != nil, func() { s.Tools.KeepCommandLogs = nil })
	return held
}

func (c *Config) applySettings(s fileSettings, path string) error {
	pick(s.BaseURL, &c.BaseURL)
	// a named variable keeps the secret out of the file; api_key still works for a single-user box
	pick(s.APIKey, &c.APIKey)
	if s.APIKeyEnv != nil {
		if fromEnv := os.Getenv(*s.APIKeyEnv); fromEnv != "" {
			c.APIKey = fromEnv
		}
	}
	pick(s.Model, &c.Model)
	pick(s.MaxTurns, &c.MaxTurns)
	pick(s.MaxTokens, &c.MaxTokens)
	pick(s.Context.SafetyMargin, &c.SafetyMargin)
	pick(s.Tools.ReadLines, &c.ReadLines)
	pick(s.Tools.SearchLimit, &c.SearchLimit)
	if s.Thinking != nil {
		c.Think = *s.Thinking
	}
	if s.Verify != nil {
		c.NoVerify = !*s.Verify
	}
	if s.AssumeYes != nil {
		c.AssumeYes = *s.AssumeYes
	}
	if s.Context.ReserveOutput != nil {
		c.MaxTokens = *s.Context.ReserveOutput
	}
	if s.KeepSessions != nil {
		d, err := time.ParseDuration(*s.KeepSessions)
		if err != nil {
			return fmt.Errorf(`%s: keep_sessions %q is not a duration like "720h"`, path, *s.KeepSessions)
		}
		c.KeepSessions = d
	}
	if s.Tools.ExecTimeoutSec != nil {
		c.ExecTimeout = time.Duration(*s.Tools.ExecTimeoutSec) * time.Second
	}
	pick(s.Tools.CommandLogDir, &c.CommandLogDir)
	if s.Tools.KeepCommandLogs != nil {
		d, err := time.ParseDuration(*s.Tools.KeepCommandLogs)
		if err != nil {
			return fmt.Errorf("%s: keep_command_logs %q is not a duration like \"24h\"", path, *s.Tools.KeepCommandLogs)
		}
		c.KeepCommandLogs = d
	}
	if s.Audit != nil {
		c.LogAudit = *s.Audit
	}
	if s.Retain != nil {
		if *s.Retain <= 0 {
			return fmt.Errorf("%s: audit_retention_days must be a whole number of days above zero", path)
		}
		c.KeepAudit = time.Duration(*s.Retain) * 24 * time.Hour
	}
	if len(s.Permissions.Allow) > 0 {
		c.Allow = append(c.Allow, s.Permissions.Allow...)
	}
	for model, ms := range s.Models {
		c.modelReasoning[model] = ms.SendReasoning != nil && *ms.SendReasoning
	}
	return nil
}

func pick[T any](from, to *T) {
	if from != nil {
		*to = *from
	}
}
