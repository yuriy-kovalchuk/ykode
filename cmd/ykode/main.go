// Command ykode is a terminal coding agent for an OpenAI-compatible server such as llama-server.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/audit"
	"github.com/yuriy-kovalchuk/ykode/internal/cli"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/home"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/session"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
	"github.com/yuriy-kovalchuk/ykode/internal/tui"
)

const startupProbeTimeout = 10 * time.Second

// version is stamped at build time with -X main.version=...
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ykode:", err)
		os.Exit(1)
	}
}

// run wires the pieces together; the context here only bounds the startup probe.
func run() error {
	if len(os.Args) > 1 && os.Args[1] == "history" {
		return historyCommand(os.Args[2:], os.Stdout)
	}
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if cfg.Version {
		fmt.Printf("ykode %s\n", version)
		return nil
	}

	// a project's settings file goes only so far on its own: say what it wanted, and ask
	trusted, err := reportHeld(cfg, os.Stdin, cfg.Prompt == "" && cli.IsTerminal(os.Stdin), os.Stderr)
	if err != nil {
		return err
	}
	if trusted {
		if cfg, err = config.Load(os.Args[1:]); err != nil {
			return err
		}
	}

	// a first run gets its directory and its settings file; everything left there ages out
	prepareHome(cfg)
	store := tools.NewResultStore(cfg.ResultDir)
	undo := &tools.UndoStack{}

	client := llm.NewClient(cfg.BaseURL, cfg.APIKey)
	probeCtx, cancelProbe := context.WithTimeout(context.Background(), startupProbeTimeout)
	defer cancelProbe()

	model, err := pickModel(probeCtx, client, cfg)
	if err != nil {
		return err
	}
	ctxSize := cfg.CtxSize
	if ctxSize == 0 {
		ctxSize = model.CtxSize
	}
	// the screen shows the model and the window in its status bar, so no banner to bury
	if !cfg.TUI {
		if cfg.JSON {
			fmt.Fprintf(os.Stderr, "model: %s\nroot:  %s\nctx:   %s\n\n", model.ID, cfg.Root, describeCtx(ctxSize))
		} else {
			fmt.Printf("model: %s\nroot:  %s\nctx:   %s\n\n", model.ID, cfg.Root, describeCtx(ctxSize))
		}
	}

	root, err := os.OpenRoot(cfg.Root)
	if err != nil {
		return fmt.Errorf("open root %q: %w", cfg.Root, err)
	}
	defer root.Close()

	resultBytes := tools.ResultBytesFor(ctxSize)
	instructions, err := agent.LoadInstructions(home.Dir(), cfg.Root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ykode:", err, "- continuing without them")
	}

	registry := tools.BuiltIn(root, cfg.Root, tools.Options{
		ReadLines:      cfg.ReadLines,
		SearchLimit:    cfg.SearchLimit,
		ExecTimeout:    cfg.ExecTimeout,
		CommandLogDir:  cfg.CommandLogDir,
		MaxResultBytes: resultBytes,
		Store:          store,
		Undo:           undo,
	})

	// Only the line front end needs a reader of its own. The screen reads the terminal
	// directly, and a second reader of stdin would swallow what the user types into it.
	var lines *cli.LineSource
	var confirm *cli.Confirmer
	if readsStdinLineByLine(cfg) {
		lines = cli.NewLineSource(os.Stdin)
		// -p with a pipe has nobody to answer a confirmation, so nothing outside the defaults may run
		if cfg.Prompt == "" || cli.IsTerminal(os.Stdin) {
			confirm = cli.NewConfirmer(lines, os.Stdout)
		}
	}

	var view agent.Sink = cli.NewView(os.Stdout, os.Stderr, confirm)
	var events *cli.JSONWriter
	var hub *tui.Hub
	if cfg.TUI {
		hub = tui.NewHub() // the screen answers confirmations itself
		view = hub
	}
	if cfg.JSON {
		events = cli.NewJSONWriter(os.Stdout)
		view = events
	}

	a := agent.New(client, registry, model.ID, agent.Options{
		MaxTurns:      cfg.MaxTurns,
		MaxTokens:     cfg.MaxTokens,
		CtxSize:       ctxSize,
		Think:         cfg.Think,
		AssumeYes:     cfg.AssumeYes,
		Instructions:  instructions,
		NoVerify:      cfg.NoVerify,
		SendReasoning: cfg.SendReasoningFor(model.ID),
		MaxResult:     resultBytes,
		Store:         store,
		Audit:         auditLogger(cfg),
		Session:       session.New(cfg.SessionDir, cfg.Session),
		Allow:         cfg.Allow,
		Root:          cfg.Root,
	}, view)

	if cfg.Continue != "" {
		messages, err := session.Load(cfg.SessionFile)
		if err != nil {
			return err
		}
		a.Resume(messages)
		fmt.Fprintf(os.Stderr, "ykode: resumed %s (%d messages)\n", cfg.Session, len(messages))
	}
	// what actually goes out each turn: a tool that is not offered yet is not paid for
	fmt.Fprintf(os.Stderr, "ykode: %d tool specs, %d bytes in every request\n", len(a.Specs()), a.SpecBytes())

	if cfg.Prompt == "" {
		if cfg.TUI {
			return tui.Run(context.Background(), a, hub)
		}
		return cli.NewREPL(a, lines, os.Stdout, os.Stderr).Run(context.Background())
	}
	return answerOnce(a, events, cfg, ctxSize)
}

// readsStdinLineByLine says whether the front end reads stdin a line at a time.
func readsStdinLineByLine(cfg config.Config) bool { return !cfg.TUI }

// answerOnce runs one task to completion, for scripts and the eval harness.
func answerOnce(a *agent.Agent, events *cli.JSONWriter, cfg config.Config, ctxSize int) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := a.Ask(ctx, cfg.Prompt)
	if events != nil {
		if err != nil {
			events.Failure(err)
		}
		events.Usage(a.Usage(), ctxSize)
	}
	return err
}

// pickModel resolves an empty --model to the server's first, and warns when the requested one is missing.
func pickModel(ctx context.Context, client *llm.Client, cfg config.Config) (llm.ModelInfo, error) {
	models, err := client.Models(ctx)
	if err != nil {
		return llm.ModelInfo{}, fmt.Errorf("cannot reach server at %s: %w", cfg.BaseURL, err)
	}
	if cfg.Model == "" {
		return models[0], nil
	}
	ids := make([]string, 0, len(models))
	for _, m := range models {
		if m.ID == cfg.Model {
			return m, nil
		}
		ids = append(ids, m.ID)
	}
	fmt.Fprintf(os.Stderr, "warning: %q is not listed by %s (models: %s)\n", cfg.Model, cfg.BaseURL, strings.Join(ids, ", "))
	return llm.ModelInfo{ID: cfg.Model}, nil
}

func describeCtx(tokens int) string {
	if tokens <= 0 {
		return "unknown (set --ctx to budget responses)"
	}
	return strconv.Itoa(tokens) + " tokens"
}

// prepareHome makes the per-user directory ready: the settings file a first run never wrote,
// and no leftovers from runs that finished long ago (spilled command output, the result dirs
// of finished runs, audit months past their retention).
func prepareHome(cfg config.Config) {
	if err := config.SeedSettings(); err != nil {
		fmt.Fprintln(os.Stderr, "ykode:", err)
	}

	removed := func(what string, n int, err error) {
		if err != nil {
			fmt.Fprintln(os.Stderr, "ykode:", err)
			return
		}
		if n > 0 {
			fmt.Fprintf(os.Stderr, "ykode: removed %d old %s\n", n, what)
		}
	}
	n, err := tools.PruneSpillLogs(cfg.CommandLogDir, cfg.KeepCommandLogs)
	removed("command log(s)", n, err)
	n, err = pruneConversations(cfg)
	removed("conversation(s)", n, err)
	n, err = audit.PruneOldFiles(cfg.AuditDir, cfg.KeepAudit)
	removed("audit file(s)", n, err)
}

// pruneConversations ages out saved conversations and, with each one, the tool results that run
// saved for recall: a result is only reachable by the conversation that names its id, so the two
// survive exactly as long as each other.
func pruneConversations(cfg config.Config) (int, error) {
	entries, err := os.ReadDir(cfg.SessionDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) { // nothing saved yet is not an error
		return 0, fmt.Errorf("clean old conversations in %s: %w", cfg.SessionDir, err)
	}
	results := filepath.Dir(cfg.ResultDir)
	removed := 0
	alive := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		id := strings.TrimSuffix(name, ".jsonl")
		info, err := e.Info()
		if err != nil {
			return removed, fmt.Errorf("stat %s: %w", name, err)
		}
		if time.Since(info.ModTime()) < cfg.KeepSessions {
			alive[id] = true
			continue
		}
		if err := os.Remove(filepath.Join(cfg.SessionDir, name)); err != nil {
			return removed, fmt.Errorf("remove %s: %w", name, err)
		}
		if err := os.RemoveAll(filepath.Join(results, id)); err != nil {
			return removed, fmt.Errorf("remove the results of %s: %w", name, err)
		}
		removed++
	}

	// a run's results are only reachable by the conversation that names their ids, so a set of
	// them whose conversation is gone is unreachable and can go now
	dirs, err := os.ReadDir(results)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return removed, fmt.Errorf("clean unreachable results in %s: %w", results, err)
	}
	for _, d := range dirs {
		if !d.IsDir() || !strings.HasPrefix(d.Name(), "s") || alive[d.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(results, d.Name())); err != nil {
			return removed, fmt.Errorf("remove %s: %w", d.Name(), err)
		}
		removed++
	}
	return removed, nil
}

// auditLogger writes the tool-call log unless a setting turned it off.
func auditLogger(cfg config.Config) *audit.Logger {
	if !cfg.LogAudit {
		return nil
	}
	return audit.New(cfg.AuditDir, cfg.Session)
}

// historyCommand answers `ykode history [flags] [what]`: what the runs did, outside a session.
// Flags come first, then what to look for - a tool name, failed, outside, or an id like r12.
func historyCommand(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("ykode history", flag.ContinueOnError)
	all := fs.Bool("all", false, "every run of this root, not only the last one")
	failed := fs.Bool("failed", false, "only calls that failed or were refused")
	outside := fs.Bool("outside", false, "only calls that reached outside the root")
	since := fs.String("since", "", "only calls in the last stretch, like 24h or 90m")
	root := fs.String("root", ".", "which project root to report on")
	asJSON := fs.Bool("json", false, "one JSON object per call")
	limit := fs.Int("limit", auditLimit, "how many calls to show")
	if err := fs.Parse(args); err != nil {
		return err
	}
	what := strings.Join(fs.Args(), " ")
	if what == "" {
		if *failed {
			what = "failed"
		}
		if *outside {
			what = "outside"
		}
	}
	window, err := time.ParseDuration(*since)
	if *since != "" && err != nil {
		return fmt.Errorf("--since %q is not a duration like 24h", *since)
	}
	settings, err := config.Load([]string{"--root", *root})
	if err != nil {
		return err
	}
	entries, damaged, err := loggedScope(settings, *all, window)
	if err != nil {
		return err
	}
	if damaged > 0 && *asJSON {
		fmt.Fprintf(os.Stderr, "ykode: %d log lines could not be read\n", damaged)
	}
	if len(entries) == 0 {
		return errors.New("nothing logged in " + settings.AuditDir)
	}
	if *asJSON {
		return loggedJSON(out, entries, what, *limit)
	}
	fmt.Fprintln(out, audit.Answer(entries, what, *limit, damaged))
	return nil
}

// loggedScope reads the run asked about: the newest one, or every run of this root, inside the window.
func loggedScope(cfg config.Config, all bool, since time.Duration) ([]audit.Entry, int, error) {
	logger := audit.New(cfg.AuditDir, "")
	var entries []audit.Entry
	var damaged int
	var err error
	if all {
		entries, damaged, err = logger.ReadRoot(cfg.Root)
	} else {
		entries, damaged, err = logger.Read(logger.LatestSession())
	}
	if err != nil || since <= 0 {
		return entries, damaged, err
	}
	cutoff := time.Now().Add(-since)
	kept := entries[:0]
	for _, e := range entries {
		if !e.Time.Before(cutoff) {
			kept = append(kept, e)
		}
	}
	return kept, damaged, nil
}

func loggedJSON(out io.Writer, entries []audit.Entry, what string, limit int) error {
	for _, e := range audit.Match(entries, what, limit) {
		raw, err := json.Marshal(e)
		if err != nil {
			return err
		}
		fmt.Fprintln(out, string(raw))
	}
	return nil
}

// auditLimit caps how many calls one history listing shows.
const auditLimit = 25
