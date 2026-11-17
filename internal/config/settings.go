package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/home"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"gopkg.in/yaml.v3"
)

// fileSettings mirrors settings.yaml; a non-nil pointer means the file set that key.
type fileSettings struct {
	BaseURL   *string `yaml:"base_url,omitempty"`
	APIKey    *string `yaml:"api_key,omitempty"`
	APIKeyEnv *string `yaml:"api_key_env,omitempty"` // name of the variable that holds it, which beats the key
	// retired: still read so an old file loads, no longer used
	Audit         *bool   `yaml:"audit,omitempty"`
	Retain        *int    `yaml:"audit_retention_days,omitempty"`
	Sandbox       *bool   `yaml:"sandbox,omitempty"`
	Verify        *bool   `yaml:"verify,omitempty"` // retired: asked for a tool on a text-only answer
	KeepSessions  *string `yaml:"keep_sessions,omitempty"`
	Model         *string `yaml:"model,omitempty"`
	MaxTurns      *int    `yaml:"max_turns,omitempty"`
	MaxTokens     *int    `yaml:"max_tokens,omitempty"`
	ThinkingLevel *string `yaml:"thinking_level,omitempty"`
	AssumeYes     *bool   `yaml:"assume_yes,omitempty"`
	Mouse         *bool   `yaml:"mouse,omitempty"` // retired: the screen no longer takes the pointer, so the terminal always selects and scrolls
	StreamIdle    *int    `yaml:"stream_idle_sec,omitempty"`
	Context       struct {
		SafetyMargin *int `yaml:"safety_margin,omitempty"`
	} `yaml:"context,omitempty"`
	Tools struct {
		ReadLines       *int    `yaml:"read_lines,omitempty"`
		SearchLimit     *int    `yaml:"search_limit,omitempty"` // retired: search_text is gone
		ExecTimeoutSec  *int    `yaml:"exec_timeout_sec,omitempty"`
		CommandLogDir   *string `yaml:"command_log_dir,omitempty"`
		KeepCommandLogs *string `yaml:"keep_command_logs,omitempty"`
	} `yaml:"tools,omitempty"`
	Permissions struct {
		Allow []string `yaml:"allow,omitempty"`
	} `yaml:"permissions,omitempty"`
	Models map[string]struct {
		Thinking *llm.Sampling `yaml:"thinking,omitempty"`
		NoThink  *llm.Sampling `yaml:"no_think,omitempty"`
	} `yaml:"models,omitempty"`
}

// settingsIn is the settings file a directory holds; both the home folder and a project's .ykode use it.
func settingsIn(dir string) string {
	return filepath.Join(dir, "settings.yaml")
}

// SeedSettings writes the defaults when nothing is there yet, and never touches an existing file
func SeedSettings() error {
	path := home.Settings()
	if _, err := os.Stat(settingsIn(home.Dir())); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	raw, err := yaml.Marshal(builtinSettings())
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// readSettings layers ~/.ykode/settings.yaml under the project's own settings file.
func (c *Config) readSettings() error {
	if err := c.applyFile(settingsIn(home.Dir()), false); err != nil {
		return err
	}
	return c.applyFile(LocalSettings(c.Root), true)
}

// applyFile layers one settings file over the rest; a project's waits until it is trusted
func (c *Config) applyFile(path string, project bool) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var s fileSettings
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	// a key the shape has no field for fails the file, naming file and key so the fix is one edit
	dec.KnownFields(true)
	if err := dec.Decode(&s); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: %s", path, describeDecode(raw, err))
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

// describeDecode turns a yaml complaint into one naming the setting, since that file is edited by hand
func describeDecode(raw []byte, err error) string {
	msg := err.Error()
	var names []string
	for _, line := range linesNamed(msg) {
		if key := keyAtLine(raw, line); key != "" && !contains(names, key) {
			names = append(names, key)
		}
	}
	if len(names) == 0 {
		return msg
	}
	return strings.Join(names, ", ") + ": " + msg
}

// linesNamed pulls the "line 12" mentions out of a yaml error.
func linesNamed(msg string) []int {
	var lines []int
	for i := 0; i+5 < len(msg); i++ {
		if msg[i:i+5] != "line " {
			continue
		}
		j := i + 5
		for j < len(msg) && msg[j] >= '0' && msg[j] <= '9' {
			j++
		}
		if j > i+5 {
			if n, err := strconv.Atoi(msg[i+5 : j]); err == nil {
				lines = append(lines, n)
			}
			i = j
		}
	}
	return lines
}

// keyAtLine is the setting a line belongs to, dotted as the table names it, from its indentation
func keyAtLine(raw []byte, line int) string {
	if line <= 0 {
		return ""
	}
	type entry struct {
		indent int
		name   string
	}
	var path []entry
	for i, text := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, _, found := strings.Cut(trimmed, ":")
		if !found {
			continue
		}
		indent := len(text) - len(strings.TrimLeft(text, " "))
		for len(path) > 0 && indent <= path[len(path)-1].indent {
			path = path[:len(path)-1]
		}
		names := make([]string, 0, len(path)+1)
		for _, e := range path {
			names = append(names, e.name)
		}
		names = append(names, strings.TrimSpace(key))
		if i+1 == line {
			// the leaf counts too: max_turns rather than only the section it sits in
			return strings.Join(names, ".")
		}
		path = append(path, entry{indent: indent, name: strings.TrimSpace(key)})
	}
	return ""
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
	if s.BaseURL != nil {
		c.EndpointSet = true
	}
	// a named variable keeps the secret out of the file; api_key still works for a single-user box
	pick(s.APIKey, &c.APIKey)
	pick(s.APIKeyEnv, &c.APIKeyEnv)
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
	pick(s.ThinkingLevel, &c.ThinkingLevel)
	if s.AssumeYes != nil {
		c.AssumeYes = *s.AssumeYes
	}
	pick(s.Tools.CommandLogDir, &c.CommandLogDir)
	if err := errors.Join(
		keepFor(path, "keep_sessions", s.KeepSessions, "720h", &c.KeepSessions),
		keepFor(path, "keep_command_logs", s.Tools.KeepCommandLogs, "24h", &c.KeepCommandLogs),
		countOf(path, "stream_idle_sec", s.StreamIdle, time.Second, &c.StreamIdle),
		countOf(path, "exec_timeout_sec", s.Tools.ExecTimeoutSec, time.Second, &c.ExecTimeout),
	); err != nil {
		return err
	}
	if len(s.Permissions.Allow) > 0 {
		c.Allow = append(c.Allow, s.Permissions.Allow...)
	}
	for model, ms := range s.Models {
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
