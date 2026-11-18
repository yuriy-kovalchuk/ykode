package config

import (
	"flag"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// flagValues carries only what the command line said; unset fields stay zero.
type flagValues struct {
	BaseURL       string
	Model         string
	Root          string
	MaxTurns      int
	MaxTokens     int
	CtxSize       int
	ThinkingLevel string
	NoThink       bool
	Yes           bool
	Prompt        string
	Continue      string
	Version       bool
}

// given reports which flags the user actually typed, and nothing at all when none of them were.
func given(fs *flag.FlagSet) map[string]bool {
	set := map[string]bool{}
	fs.Visit(func(fl *flag.Flag) { set[fl.Name] = true })
	if len(set) == 0 {
		return nil
	}
	return set
}

// applyFlags applies the flags the user typed; a bool flag only ever switches its setting on (or thinking off).
func (c *Config) applyFlags(f flagValues, set map[string]bool) {
	setters := map[string]func(){
		"base-url":   func() { c.BaseURL, c.EndpointSet = f.BaseURL, true },
		"model":      func() { c.Model = f.Model },
		"max-turns":  func() { c.MaxTurns = f.MaxTurns },
		"max-tokens": func() { c.MaxTokens = f.MaxTokens },
		"ctx":        func() { c.CtxSize = f.CtxSize },
		"thinking":   func() { c.ThinkingLevel = f.ThinkingLevel },
		"yes":        func() { c.AssumeYes = c.AssumeYes || f.Yes },
		"p":          func() { c.Prompt = f.Prompt },
		"continue":   func() { c.Continue = f.Continue },
		"version":    func() { c.Version = f.Version },
	}
	for name := range set {
		if apply, ok := setters[name]; ok {
			apply()
		}
	}
	// --thinking and --no-think set one thing: a depth spelled out wins whichever order they came in
	if set["no-think"] && f.NoThink && !set["thinking"] {
		c.ThinkingLevel = llm.ThinkingOff
	}
}
