package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/home"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// fileSettings mirrors settings.json; a non-nil pointer means the file set that key.
type fileSettings struct {
	BaseURL       *string `json:"base_url,omitempty"`
	APIKey        *string `json:"api_key,omitempty"`
	APIKeyEnv     *string `json:"api_key_env,omitempty"` // name of the variable that holds it, which beats the key
	Audit         *bool   `json:"audit,omitempty"`
	Retain        *int    `json:"audit_retention_days,omitempty"`
	KeepSessions  *string `json:"keep_sessions,omitempty"`
	Model         *string `json:"model,omitempty"`
	MaxTurns      *int    `json:"max_turns,omitempty"`
	MaxTokens     *int    `json:"max_tokens,omitempty"`
	ThinkingLevel *string `json:"thinking_level,omitempty"`
	AssumeYes     *bool   `json:"assume_yes,omitempty"`
	Sandbox       *bool   `json:"sandbox,omitempty"`
	Verify        *bool   `json:"verify,omitempty"`
	StreamIdle    *int    `json:"stream_idle_sec,omitempty"`
	Context       struct {
		SafetyMargin *int `json:"safety_margin,omitempty"`
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
		SendReasoning *bool         `json:"send_reasoning,omitempty"`
		Thinking      *llm.Sampling `json:"thinking,omitempty"`
		NoThink       *llm.Sampling `json:"no_think,omitempty"`
	} `json:"models,omitempty"`
}

// builtinSettings are the defaults ykode ships with, in the shape of the file, so the
// settings.json a first run writes cannot disagree with what the code would have done.
func builtinSettings() fileSettings {
	d := defaults() // the same list the code resolves from, so the file cannot contradict it
	var s fileSettings
	s.BaseURL, s.Model, s.MaxTurns, s.MaxTokens = p(d.BaseURL), p("default"), p(d.MaxTurns), p(d.MaxTokens)
	s.ThinkingLevel, s.Verify, s.AssumeYes, s.Sandbox = p(d.ThinkingLevel), p(!d.NoVerify), p(d.AssumeYes), p(d.Sandbox)
	s.Audit, s.Retain = p(d.LogAudit), p(int(d.KeepAudit/(24*time.Hour)))
	s.Context.SafetyMargin = p(d.SafetyMargin)
	s.Tools.ReadLines, s.Tools.SearchLimit = p(d.ReadLines), p(d.SearchLimit)
	s.KeepSessions = p(hours(d.KeepSessions))
	s.Tools.ExecTimeoutSec = p(int(d.ExecTimeout / time.Second))
	s.StreamIdle = p(int(d.StreamIdle / time.Second))
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
			c.HeldKeys, c.HeldValues = stripUnsafe(&s)
		}
		sum := sha256.Sum256(raw)
		c.ProjectHash = hex.EncodeToString(sum[:])
	}
	return c.applySettings(s, path)
}

// stripUnsafe holds back what a cloned project must not decide alone, and shows what it asked for.
func stripUnsafe(s *fileSettings) (names, shown []string) {
	drop := func(name string, isSet bool, value any, clear func()) {
		if !isSet {
			return
		}
		raw, _ := json.Marshal(value)
		names = append(names, name)
		shown = append(shown, name+" = "+string(raw))
		clear()
	}
	drop("base_url", s.BaseURL != nil, s.BaseURL, func() { s.BaseURL = nil })
	drop("api_key", s.APIKey != nil, "(not shown)", func() { s.APIKey = nil })
	drop("api_key_env", s.APIKeyEnv != nil, s.APIKeyEnv, func() { s.APIKeyEnv = nil })
	drop("model", s.Model != nil, s.Model, func() { s.Model = nil })
	drop("assume_yes", s.AssumeYes != nil, s.AssumeYes, func() { s.AssumeYes = nil })
	drop("sandbox", s.Sandbox != nil, s.Sandbox, func() { s.Sandbox = nil })
	drop("audit", s.Audit != nil, s.Audit, func() { s.Audit = nil })
	drop("audit_retention_days", s.Retain != nil, s.Retain, func() { s.Retain = nil })
	drop("keep_sessions", s.KeepSessions != nil, s.KeepSessions, func() { s.KeepSessions = nil }) // every project's conversations share one folder
	drop("permissions.allow", s.Permissions.Allow != nil, s.Permissions.Allow, func() { s.Permissions.Allow = nil })
	drop("tools.command_log_dir", s.Tools.CommandLogDir != nil, s.Tools.CommandLogDir, func() { s.Tools.CommandLogDir = nil })
	drop("tools.keep_command_logs", s.Tools.KeepCommandLogs != nil, s.Tools.KeepCommandLogs, func() { s.Tools.KeepCommandLogs = nil })
	return names, shown
}

// keepFor reads a retention setting when it is set; zero or less would delete everything at the next start.
func keepFor(path, key string, value *string, example string, to *time.Duration) error {
	if value == nil {
		return nil
	}
	d, err := time.ParseDuration(*value)
	if err != nil {
		return fmt.Errorf(`%s: %s %q is not a duration like "%s"`, path, key, *value, example)
	}
	if d <= 0 {
		return fmt.Errorf(`%s: %s %q would delete everything at the next start; use a duration above zero like "%s"`, path, key, *value, example)
	}
	*to = d
	return nil
}

// countOf reads a whole number of units when it is set; zero or less is refused by name.
func countOf(path, key string, value *int, unit time.Duration, to *time.Duration) error {
	if value == nil {
		return nil
	}
	if *value <= 0 {
		return fmt.Errorf("%s: %s must be a whole number above zero, got %d", path, key, *value)
	}
	*to = time.Duration(*value) * unit
	return nil
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
	pick(s.ThinkingLevel, &c.ThinkingLevel)
	if s.Verify != nil {
		c.NoVerify = !*s.Verify
	}
	if s.AssumeYes != nil {
		c.AssumeYes = *s.AssumeYes
	}
	pick(s.Sandbox, &c.Sandbox)
	pick(s.Tools.CommandLogDir, &c.CommandLogDir)
	if s.Audit != nil {
		c.LogAudit = *s.Audit
	}
	if err := errors.Join(
		keepFor(path, "keep_sessions", s.KeepSessions, "720h", &c.KeepSessions),
		keepFor(path, "keep_command_logs", s.Tools.KeepCommandLogs, "24h", &c.KeepCommandLogs),
		countOf(path, "stream_idle_sec", s.StreamIdle, time.Second, &c.StreamIdle),
		countOf(path, "exec_timeout_sec", s.Tools.ExecTimeoutSec, time.Second, &c.ExecTimeout),
		countOf(path, "audit_retention_days", s.Retain, 24*time.Hour, &c.KeepAudit),
	); err != nil {
		return err
	}
	if len(s.Permissions.Allow) > 0 {
		c.Allow = append(c.Allow, s.Permissions.Allow...)
	}
	for model, ms := range s.Models {
		c.modelReasoning[model] = ms.SendReasoning != nil && *ms.SendReasoning
		c.modelSampling[model] = [2]llm.Sampling{deref(ms.Thinking), deref(ms.NoThink)}
	}
	return nil
}

func deref[T any](v *T) (zero T) {
	if v == nil {
		return zero
	}
	return *v
}

func pick[T any](from, to *T) {
	if from != nil {
		*to = *from
	}
}
