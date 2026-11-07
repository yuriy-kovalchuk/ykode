// Package config resolves ykode's settings: built-in defaults, then settings.json, then env, then flags.
package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/yuriy-kovalchuk/ykode/internal/session"
	"strings"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/home"
)

// Defaults used when no setting, env var or flag says otherwise.
const (
	defaultBaseURL     = "http://127.0.0.1:8080/v1"
	defaultMaxTurns    = 25
	defaultMaxTokens   = 4096
	defaultReadLines   = 150
	defaultSearchLimit = 50
	defaultExecTimeout = 60 * time.Second
	defaultLogKeep     = 24 * time.Hour
	defaultSafety      = 256
	sessionStamp       = "20060102-150405"
)

// Config is the fully resolved set of settings.
type Config struct {
	BaseURL         string // OpenAI-compatible base, no trailing slash
	APIKey          string // sent as a bearer token when set
	Model           string // empty means "whatever the server lists first"
	Root            string // absolute project root
	MaxTurns        int
	MaxTokens       int // cap on one model response
	CtxSize         int // server context window; 0 = read it from the server
	SafetyMargin    int // tokens kept free for the reply
	Think           bool
	AssumeYes       bool
	NoVerify        bool // do not push a text-only answer back into the tools
	SendReasoning   bool // repeat earlier thinking back to models whose template expects it
	ReadLines       int  // read_file window when the model does not ask for one
	SearchLimit     int  // search_text match limit when the model does not ask for one
	ExecTimeout     time.Duration
	CommandLogDir   string // where a cut command's full output is saved
	ResultDir       string // one run's shortened tool results, kept so recall can hand them back
	AuditDir        string // append-only tool-call log, one file per month
	Session         string // this run's id, shared by the results, the audit log and sessions
	KeepCommandLogs time.Duration
	Prompt          string          // one task to answer and exit; empty runs the REPL
	JSON            bool            // print the event stream as JSON lines
	Audit           bool            // print what the last run did and exit
	Allow           []string        // never ask about these: a tool name, or "exec:go test"
	Continue        string          // resume a saved conversation: "last" or its id
	SessionDir      string          // where conversations are saved
	SessionFile     string          // the conversation this run reads and writes
	Version         bool            // print the version and exit
	modelReasoning  map[string]bool // per-model send_reasoning
}

// flagValues carries only what the command line said; unset fields stay zero.
type flagValues struct {
	BaseURL       string
	APIKey        string
	Model         string
	Root          string
	MaxTurns      int
	MaxTokens     int
	CtxSize       int
	NoThink       bool
	Yes           bool
	NoVerify      bool
	SendReasoning bool
	Prompt        string
	JSON          bool
	Audit         bool
	Continue      string
	Version       bool
}

// Load parses args (os.Args[1:]). flag.ErrHelp is returned for -h.
func Load(args []string) (Config, error) {
	var f flagValues
	fs := flag.NewFlagSet("ykode", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: ykode [--base-url URL] [--api-key KEY] [--model ID] [--root DIR] [--max-turns N] [--max-tokens N] [--ctx N] [--no-think] [--no-verify] [--send-reasoning] [--yes] [-p TASK] [--json] [--audit] [--continue ID] [--version]")
		fs.PrintDefaults()
	}
	fs.StringVar(&f.BaseURL, "base-url", "", "base URL of an OpenAI-compatible server ($LOCAL_LLM_URL, default "+defaultBaseURL+")")
	fs.StringVar(&f.APIKey, "api-key", "", "bearer token for servers that ask for one ($LOCAL_LLM_API_KEY)")
	fs.StringVar(&f.Model, "model", "", "model id; default is the first from /models ($LOCAL_LLM_MODEL)")
	fs.StringVar(&f.Root, "root", ".", "project root; every file tool is confined to it")
	fs.IntVar(&f.MaxTurns, "max-turns", 0, "max model round-trips per user request (default 25)")
	fs.IntVar(&f.MaxTokens, "max-tokens", 0, "max tokens per model response (default 4096)")
	fs.IntVar(&f.CtxSize, "ctx", 0, "server context window in tokens; 0 reads it from the server")
	fs.BoolVar(&f.NoThink, "no-think", false, "skip the model's thinking phase")
	fs.BoolVar(&f.Yes, "yes", false, "skip in-project confirmations (paths outside --root are always asked about)")
	fs.BoolVar(&f.NoVerify, "no-verify", false, "do not ask the model to re-answer with a tool when it answered from memory")
	fs.BoolVar(&f.SendReasoning, "send-reasoning", false, "send earlier thinking back to the model, which some chat templates expect")
	fs.StringVar(&f.Prompt, "p", "", "answer one task and exit instead of opening a session")
	fs.BoolVar(&f.JSON, "json", false, "with -p: print the event stream as JSON lines")
	fs.BoolVar(&f.Audit, "audit", false, "print what the last run did, tool by tool, and exit")
	fs.StringVar(&f.Continue, "continue", "", "resume a saved conversation: last, or the id from a previous run")
	fs.BoolVar(&f.Version, "version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	cfg := defaults()
	root, err := filepath.Abs(f.Root)
	if err != nil {
		return Config{}, fmt.Errorf("--root %q: %w", f.Root, err)
	}
	cfg.Root = root
	if err := cfg.readSettings(); err != nil {
		return Config{}, err
	}
	cfg.applyEnv()
	cfg.applyFlags(f, given(fs))
	cfg.Version = f.Version

	if err := cfg.normalize(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func defaults() Config {
	return Config{
		BaseURL:         defaultBaseURL,
		MaxTurns:        defaultMaxTurns,
		MaxTokens:       defaultMaxTokens,
		SafetyMargin:    defaultSafety,
		Think:           true,
		ReadLines:       defaultReadLines,
		SearchLimit:     defaultSearchLimit,
		ExecTimeout:     defaultExecTimeout,
		KeepCommandLogs: defaultLogKeep,
		modelReasoning:  map[string]bool{},
	}
}

// given reports which flags the user actually typed.
func given(fs *flag.FlagSet) map[string]bool {
	set := map[string]bool{}
	fs.Visit(func(fl *flag.Flag) { set[fl.Name] = true })
	return set
}

func (c *Config) applyFlags(f flagValues, set map[string]bool) {
	if set["base-url"] {
		c.BaseURL = f.BaseURL
	}
	if set["api-key"] {
		c.APIKey = f.APIKey
	}
	if set["model"] {
		c.Model = f.Model
	}
	if set["max-turns"] {
		c.MaxTurns = f.MaxTurns
	}
	if set["max-tokens"] {
		c.MaxTokens = f.MaxTokens
	}
	if set["ctx"] {
		c.CtxSize = f.CtxSize
	}
	if set["no-think"] && f.NoThink {
		c.Think = false
	}
	if set["yes"] && f.Yes {
		c.AssumeYes = true
	}
	if set["no-verify"] && f.NoVerify {
		c.NoVerify = true
	}
	if set["send-reasoning"] && f.SendReasoning {
		c.SendReasoning = true
	}
	if set["p"] {
		c.Prompt = f.Prompt
	}
	if set["json"] && f.JSON {
		c.JSON = true
	}
	if set["audit"] && f.Audit {
		c.Audit = true
	}
	if set["continue"] {
		c.Continue = f.Continue
	}
}

func (c *Config) applyEnv() {
	if v := os.Getenv("LOCAL_LLM_URL"); v != "" {
		c.BaseURL = v
	}
	if v := os.Getenv("LOCAL_LLM_MODEL"); v != "" {
		c.Model = v
	}
	if v := os.Getenv("LOCAL_LLM_API_KEY"); v != "" {
		c.APIKey = v
	}
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
	if c.ReadLines < 1 || c.SearchLimit < 1 {
		return fmt.Errorf("read_lines and search_limit must be at least 1, got %d and %d", c.ReadLines, c.SearchLimit)
	}
	if c.ExecTimeout <= 0 {
		return fmt.Errorf("exec_timeout_sec must be positive, got %s", c.ExecTimeout)
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

// SendReasoningFor reports a per-model send_reasoning setting.
func (c *Config) SendReasoningFor(model string) bool {
	return c.SendReasoning || c.modelReasoning[model]
}

// sessionIDPattern guards against a --continue argument that is not an id but a path.
var sessionIDPattern = regexp.MustCompile(`^s[0-9]{8}-[0-9]{6}$`)

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
