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

// Cards are every model a settings file wrote something about, so a switch mid-run can pick up the
// model it is switching to rather than keeping the numbers of the one it is leaving. Only the models
// named in the file appear: an unnamed one carries nothing known about it.
func (c *Config) Cards() map[string]llm.ModelCard {
	if len(c.modelSampling) == 0 && len(c.modelReasoning) == 0 {
		return nil
	}
	cards := make(map[string]llm.ModelCard, len(c.modelSampling)+len(c.modelReasoning))
	for model := range c.modelSampling {
		cards[model] = llm.ModelCard{Thinking: c.modelSampling[model][0], NoThink: c.modelSampling[model][1]}
	}
	for model, on := range c.modelReasoning {
		// the file may have said only that this template wants thinking sent back, with no numbers:
		// then the card is empty for the numbers and the server's own defaults are what run
		card := cards[model]
		card.SendReasoning = c.SendReasoning || on
		cards[model] = card
	}
	return cards
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
	s.Context.SafetyMargin = p(d.SafetyMargin)
	s.Tools.ReadLines = p(d.ReadLines)
	s.KeepSessions = p(hours(d.KeepSessions))
	s.Tools.ExecTimeoutSec = p(int(d.ExecTimeout / time.Second))
	s.StreamIdle = p(int(d.StreamIdle / time.Second))
	// command_log_dir is left out rather than seeded empty: normalize turns an empty value into the
	// folder under home, and the settings table then reads that resolved path as a value the file set -
	// the one seeded key that does not repeat its default verbatim, and so the only one reported as
	// chosen. A key left out is the file's own way of saying "use the default".
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
