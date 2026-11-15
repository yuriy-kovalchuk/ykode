package config

import (
	"fmt"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"time"
)

// SamplingFor returns a model's sampling with thinking on and off; unset fields stay nil.
func (c *Config) SamplingFor(model string) (thinking, noThink llm.Sampling) {
	s := c.modelSampling[model]
	return s[0], s[1]
}

// SendReasoningFor reports a per-model send_reasoning setting.
func (c *Config) SendReasoningFor(model string) bool {
	return c.SendReasoning || c.modelReasoning[model]
}

// builtinSettings are the defaults ykode ships with, in the shape of the file, so the
// settings.yaml a first run writes cannot disagree with what the code would have done.
func builtinSettings() fileSettings {
	d := defaults() // the same list the code resolves from, so the file cannot contradict it
	var s fileSettings
	// left out, so the first full-screen run asks which server to use
	s.MaxTurns, s.MaxTokens = p(d.MaxTurns), p(d.MaxTokens)
	s.ThinkingLevel, s.AssumeYes = p(d.ThinkingLevel), p(d.AssumeYes)
	s.Mouse = p(d.Mouse)
	s.Context.SafetyMargin = p(d.SafetyMargin)
	s.Tools.ReadLines = p(d.ReadLines)
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
