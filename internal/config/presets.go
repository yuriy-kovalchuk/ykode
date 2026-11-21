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

// Cards are every model a settings file gave sampling for, so a switch takes the new model's numbers
func (c *Config) Cards() map[string]llm.ModelCard {
	if len(c.modelSampling) == 0 {
		return nil
	}
	cards := make(map[string]llm.ModelCard, len(c.modelSampling))
	for model, s := range c.modelSampling {
		cards[model] = llm.ModelCard{Thinking: s[0], NoThink: s[1]}
	}
	return cards
}

// builtinSettings are the shipped defaults in the file's shape, so the seed cannot disagree with the code
func builtinSettings() fileSettings {
	d := defaults() // the same list the code resolves from, so the file cannot contradict it
	var s fileSettings
	// left out, so the first full-screen run asks which server to use
	s.MaxTurns, s.MaxTokens = p(d.MaxTurns), p(d.MaxTokens)
	s.ThinkingLevel, s.AssumeYes = p(d.ThinkingLevel), p(d.AssumeYes)
	s.Theme, s.SaveCache = p(d.Theme), p(d.SaveServerCache)
	s.Context.SafetyMargin = p(d.SafetyMargin)
	s.Tools.ReadLines = p(d.ReadLines)
	s.KeepSessions = p(hours(d.KeepSessions))
	s.Tools.ExecTimeoutSec = p(int(d.ExecTimeout / time.Second))
	s.StreamIdle = p(int(d.StreamIdle / time.Second))
	// command_log_dir is left out, not seeded empty: an empty value resolves and then reads as chosen
	s.Tools.KeepCommandLogs = p(hours(d.KeepCommandLogs))
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
