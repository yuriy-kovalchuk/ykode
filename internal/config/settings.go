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
	BaseURL   *string `json:"base_url"`
	APIKey    *string `json:"api_key"`
	APIKeyEnv *string `json:"api_key_env"` // name of the variable that holds it, which beats the key
	Audit     *bool   `json:"audit"`
	Retain    *int    `json:"audit_retention_days"`
	Model     *string `json:"model"`
	MaxTurns  *int    `json:"max_turns"`
	MaxTokens *int    `json:"max_tokens"`
	Thinking  *bool   `json:"thinking"`
	AssumeYes *bool   `json:"assume_yes"`
	Verify    *bool   `json:"verify"`
	Context   struct {
		ReserveOutput *int `json:"reserve_output"`
		SafetyMargin  *int `json:"safety_margin"`
	} `json:"context"`
	Tools struct {
		ReadLines       *int    `json:"read_lines"`
		SearchLimit     *int    `json:"search_limit"`
		ExecTimeoutSec  *int    `json:"exec_timeout_sec"`
		CommandLogDir   *string `json:"command_log_dir"`
		KeepCommandLogs *string `json:"keep_command_logs"`
	} `json:"tools"`
	Permissions struct {
		Allow []string `json:"allow"`
	} `json:"permissions"`
	Models map[string]struct {
		SendReasoning *bool `json:"send_reasoning"`
	} `json:"models"`
}

// readSettings layers ~/.ykode/settings.json under <root>/.ykode/settings.json.
func (c *Config) readSettings() error {
	for _, path := range []string{home.Settings(), filepath.Join(c.Root, ".ykode", "settings.json")} {
		if err := c.applyFile(path); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) applyFile(path string) error {
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
	if s.Thinking != nil && !*s.Thinking {
		c.Think = false
	}
	if s.Verify != nil && !*s.Verify {
		c.NoVerify = true
	}
	if s.AssumeYes != nil {
		c.AssumeYes = *s.AssumeYes
	}
	if s.Context.ReserveOutput != nil {
		c.MaxTokens = *s.Context.ReserveOutput
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
