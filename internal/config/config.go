// Package config turns ykode's flags (each with an env fallback) into settings.
package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Defaults used when neither a flag nor its env var is set.
const (
	defaultBaseURL   = "http://127.0.0.1:8080/v1"
	defaultMaxTurns  = 25
	defaultMaxTokens = 4096
)

// Config is the fully resolved set of settings.
type Config struct {
	BaseURL   string // OpenAI-compatible base, no trailing slash
	Model     string // empty means "whatever the server lists first"
	Root      string // absolute project root
	MaxTurns  int
	MaxTokens int  // cap on one model response
	CtxSize   int  // server context window; 0 = read it from the server
	Think     bool // ask the model to think before answering
	AssumeYes bool // skip in-project confirmations
}

// Load parses args (os.Args[1:]). flag.ErrHelp is returned for -h.
func Load(args []string) (Config, error) {
	var cfg Config
	fs := flag.NewFlagSet("ykode", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: ykode [--base-url URL] [--model ID] [--root DIR] [--max-turns N] [--max-tokens N] [--ctx N] [--no-think] [--yes]")
		fs.PrintDefaults()
	}
	fs.StringVar(&cfg.BaseURL, "base-url", env("LOCAL_LLM_URL", defaultBaseURL), "base URL of an OpenAI-compatible server ($LOCAL_LLM_URL)")
	fs.StringVar(&cfg.Model, "model", env("LOCAL_LLM_MODEL", ""), "model id; empty uses the first from /models ($LOCAL_LLM_MODEL)")
	fs.StringVar(&cfg.Root, "root", ".", "project root; every file tool is confined to it")
	fs.IntVar(&cfg.MaxTurns, "max-turns", defaultMaxTurns, "max model round-trips per user request")
	fs.IntVar(&cfg.MaxTokens, "max-tokens", defaultMaxTokens, "max tokens per model response; shrunk further to fit the context window")
	fs.IntVar(&cfg.CtxSize, "ctx", 0, "server context window in tokens; 0 reads it from the server")
	fs.BoolFunc("no-think", "skip the model's thinking phase", func(string) error {
		cfg.Think = false
		return nil
	})
	fs.BoolVar(&cfg.AssumeYes, "yes", false, "skip in-project confirmations (paths outside --root are always asked about)")
	cfg.Think = true
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if err := cfg.normalize(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// normalize rejects nonsense settings and pins the root to an existing absolute directory.
func (c *Config) normalize() error {
	if c.MaxTurns < 1 {
		return fmt.Errorf("--max-turns must be at least 1, got %d", c.MaxTurns)
	}
	if c.MaxTokens < 1 {
		return fmt.Errorf("--max-tokens must be at least 1, got %d", c.MaxTokens)
	}
	if c.CtxSize < 0 {
		return fmt.Errorf("--ctx must be 0 (auto) or a positive token count, got %d", c.CtxSize)
	}
	root, err := filepath.Abs(c.Root)
	if err != nil {
		return fmt.Errorf("--root %q: %w", c.Root, err)
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
	return nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
