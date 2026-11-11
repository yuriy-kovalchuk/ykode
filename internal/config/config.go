// Package config resolves ykode's settings: built-in defaults, then settings.yaml, then env, then flags.
package config

import (
	"flag"
	"fmt"
	"path/filepath"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"time"
)

// Defaults used when no setting, env var or flag says otherwise.
const (
	defaultBaseURL       = "http://127.0.0.1:8080/v1"
	defaultMaxTurns      = 100
	defaultReadLines     = 150
	defaultSearchLimit   = 50
	defaultExecTimeout   = 60 * time.Second
	defaultStreamIdle    = 2 * time.Minute // silence before a stream counts as dead; a cold prompt may need more
	defaultLogKeep       = 24 * time.Hour
	defaultSessionKeep   = 30 * 24 * time.Hour  // a conversation, and the results recall can still name
	defaultAuditKeep     = 180 * 24 * time.Hour // six months of tool calls
	defaultSafety        = 4096                 // room left free for one reply when no cap was asked for
	defaultThinkingLevel = llm.DefaultThinkingLevel
	sessionStamp         = "20060102-150405.000" // milliseconds, so two runs started together do not share one
)

// Config is the fully resolved set of settings.
type Config struct {
	BaseURL         string   // OpenAI-compatible base, no trailing slash
	APIKey          string   // sent as a bearer token when set
	Args            []string // what Load was given, so the settings screen can read the file again
	Notices         []string // what startup found worth saying, which the full screen shows first
	EndpointSet     bool     // base_url came from a settings file, $LOCAL_LLM_URL or --base-url, not the built-in default
	Model           string   // empty means "whatever the server lists first"
	Root            string   // absolute project root
	MaxTurns        int
	MaxTokens       int    // tokens per model response; 0 asks for no cap and leaves it to the window
	CtxSize         int    // server context window; 0 = read it from the server
	SafetyMargin    int    // tokens kept free for the reply
	ThinkingLevel   string // off, minimal, low, medium, high, xhigh, max
	AssumeYes       bool
	Mouse           bool // the screen takes the mouse: the wheel scrolls, and the terminal cannot select text
	Sandbox         bool // exec_command runs confined: writes only in the project, no network
	NoVerify        bool // do not push a text-only answer back into the tools
	SendReasoning   bool // repeat earlier thinking back to models whose template expects it
	ReadLines       int  // read_file window when the model does not ask for one
	SearchLimit     int  // search_text match limit when the model does not ask for one
	ExecTimeout     time.Duration
	StreamIdle      time.Duration // how long a stream may stay silent
	CommandLogDir   string        // where a cut command's full output is saved
	ResultDir       string        // one run's shortened tool results, kept so recall can hand them back
	AuditDir        string        // append-only tool-call log, one file per month
	LogAudit        bool          // write that log at all; off only by a setting
	Session         string        // this run's id, shared by the results, the audit log and sessions
	KeepCommandLogs time.Duration
	KeepSessions    time.Duration              // how long a saved conversation survives, and its saved results
	KeepAudit       time.Duration              // how long old audit files survive
	Prompt          string                     // one task to answer and exit; empty runs the REPL
	JSON            bool                       // print the event stream as JSON lines
	TUI             bool                       // full-screen session instead of the line prompt
	Allow           []string                   // never ask about these: a tool name, or "exec:go test"
	Continue        string                     // resume a saved conversation: "last" or its id
	SessionDir      string                     // where conversations are saved
	ProjectSettings string                     // <root>/.ykode/settings.yaml, when the project ships one
	APIKeyEnv       string                     // a variable that holds the token, named by the file
	flagsGiven      map[string]bool            // which flags the command line actually typed
	HeldKeys        []string                   // what that file sets that only applies to a trusted project
	HeldValues      []string                   // the same keys with what they ask for, secrets masked
	ProjectHash     string                     // sha256 of the project file as read, so trust covers what was shown
	SessionFile     string                     // the conversation this run reads and writes
	Version         bool                       // print the version and exit
	modelReasoning  map[string]bool            // per-model send_reasoning
	modelSampling   map[string][2]llm.Sampling // per model: with thinking, without
}

// Load parses args (os.Args[1:]). flag.ErrHelp is returned for -h.
func Load(args []string) (Config, error) {
	var f flagValues
	fs := flag.NewFlagSet("ykode", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: ykode [--base-url URL] [--api-key KEY] [--model ID] [--root DIR] [--max-turns N] [--max-tokens N] [--ctx N] [--thinking LEVEL] [--no-think] [--no-verify] [--send-reasoning] [--yes] [-p TASK] [--json] [--no-tui] [--continue ID] [--version]")
		fmt.Fprintln(fs.Output(), "       ykode history [what] [--all] [--failed] [--since DUR] [--root DIR] [--json] - what the runs did, outside a session")
		fmt.Fprintln(fs.Output(), "       ykode sessions [--limit N] - the conversations that can be resumed")
		fs.PrintDefaults()
	}
	fs.StringVar(&f.BaseURL, "base-url", "", "base URL of an OpenAI-compatible server ($LOCAL_LLM_URL, default "+defaultBaseURL+")")
	fs.StringVar(&f.APIKey, "api-key", "", "bearer token for servers that ask for one ($LOCAL_LLM_API_KEY)")
	fs.StringVar(&f.Model, "model", "", "model id; default is the first from /models ($LOCAL_LLM_MODEL)")
	fs.StringVar(&f.Root, "root", ".", "project root; every file tool is confined to it")
	fs.IntVar(&f.MaxTurns, "max-turns", 0, "max model round-trips per user request (default 100)")
	fs.IntVar(&f.MaxTokens, "max-tokens", 0, "tokens per model response; 0 leaves the reply the room the window has left (default)")
	fs.IntVar(&f.CtxSize, "ctx", 0, "server context window in tokens; 0 reads it from the server")
	fs.StringVar(&f.ThinkingLevel, "thinking", "", "how deeply the model thinks: off, minimal, low, medium, high, xhigh, max")
	fs.BoolVar(&f.NoThink, "no-think", false, "shorthand for --thinking off")
	fs.BoolVar(&f.Yes, "yes", false, "skip in-project confirmations (paths outside --root are always asked about)")
	fs.BoolVar(&f.NoVerify, "no-verify", false, "do not ask the model to re-answer with a tool when it answered from memory")
	fs.BoolVar(&f.SendReasoning, "send-reasoning", false, "send earlier thinking back to the model, which some chat templates expect")
	fs.StringVar(&f.Prompt, "p", "", "answer one task and exit instead of opening a session")
	fs.BoolVar(&f.JSON, "json", false, "with -p: print the event stream as JSON lines")
	fs.BoolVar(&f.NoTUI, "no-tui", false, "a line prompt instead of the full-screen session")
	fs.StringVar(&f.Continue, "continue", "", "resume a saved conversation: last, or its id (enough of the id to be unique works); ykode sessions lists them")
	fs.BoolVar(&f.Version, "version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	cfg := defaults()
	cfg.Args = args
	root, err := filepath.Abs(f.Root)
	if err != nil {
		return Config{}, fmt.Errorf("--root %q: %w", f.Root, err)
	}
	cfg.Root = root
	if err := cfg.readSettings(); err != nil {
		return Config{}, err
	}
	cfg.flagsGiven = given(fs)
	cfg.applyEnv()
	cfg.applyFlags(f, cfg.flagsGiven)

	if err := cfg.normalize(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func defaults() Config {
	return Config{
		BaseURL:         defaultBaseURL,
		MaxTurns:        defaultMaxTurns,
		MaxTokens:       0, // no cap: the context window decides
		SafetyMargin:    defaultSafety,
		ThinkingLevel:   defaultThinkingLevel,
		ReadLines:       defaultReadLines,
		SearchLimit:     defaultSearchLimit,
		ExecTimeout:     defaultExecTimeout,
		StreamIdle:      defaultStreamIdle,
		KeepCommandLogs: defaultLogKeep,
		KeepSessions:    defaultSessionKeep,
		KeepAudit:       defaultAuditKeep,
		LogAudit:        true,
		TUI:             true, // --no-tui, -p and --json turn it off
		modelReasoning:  map[string]bool{},
		modelSampling:   map[string][2]llm.Sampling{},
	}
}

// Reload reads the settings again after one was written, keeping what belongs to this run and not to
// the file: the conversation it is writing, and what startup said.
func (c Config) Reload() (Config, error) {
	fresh, err := Load(c.Args)
	if err != nil {
		return c, err
	}
	fresh.Session, fresh.SessionFile, fresh.Continue, fresh.Notices = c.Session, c.SessionFile, c.Continue, c.Notices
	return fresh, nil
}
