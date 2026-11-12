package app

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
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

// Run is one ykode invocation, args without the program name: settings, the server, the tools and agent, then a front end.
func Run(args []string, version string) error {
	if len(args) > 0 && args[0] == "history" {
		return cli.History(args[1:], os.Stdout)
	}
	if len(args) > 0 && args[0] == "sessions" {
		return cli.Sessions(args[1:], os.Stdout)
	}
	cfg, done, err := startupConfig(args, version)
	if done || err != nil {
		return err
	}
	// piped in or out, a screen has nowhere to draw: the line prompt reads and writes the pipe
	cfg.TUI = cfg.TUI && cli.IsTerminal(os.Stdin) && cli.IsTerminal(os.Stdout)
	// the built-in localhost default is a guess: ask, rather than fail with "cannot reach server"
	if cfg.TUI && !cfg.EndpointSet {
		if cfg, err = askForServer(cfg, ""); err != nil {
			return err
		}
	}
	// a first run gets its directory and its settings file; everything left there ages out
	PrepareHome(cfg)
	client, model, ctxSize, note, err := connect(cfg)
	if err != nil && cfg.TUI {
		// the server went away or was never right: offer to fix it instead of exiting
		if cfg, err = askForServer(cfg, err.Error()); err != nil {
			return err
		}
		client, model, ctxSize, note, err = connect(cfg)
	}
	if err != nil {
		return err
	}
	if note != "" {
		cfg.Notices = append(cfg.Notices, note)
	}

	root, err := os.OpenRoot(cfg.Root)
	if err != nil {
		return fmt.Errorf("open root %q: %w", cfg.Root, err)
	}
	defer root.Close()

	instructions, err := agent.LoadInstructions(home.Dir(), cfg.Root, agent.InstructionBytes(ctxSize))
	if err != nil {
		fmt.Fprintln(os.Stderr, "ykode:", err, "- continuing without them")
	}
	toolOpts, agentOpts := Options(cfg, model.ID, ctxSize, tools.NewResultStore(cfg.ResultDir), &tools.UndoStack{})
	agentOpts.Instructions, agentOpts.Audit = instructions, auditLogger(cfg)
	agentOpts.Session = session.New(cfg.SessionDir, cfg.Session, cfg.Root)

	front := NewFrontEnd(cfg)
	a := agent.New(client, tools.BuiltIn(root, cfg.Root, toolOpts), model.ID, agentOpts, front.View)
	if cfg.Continue != "" {
		saved, err := session.LoadAll(cfg.SessionFile, cfg.Root)
		if err != nil {
			return err
		}
		messages := saved.Messages
		a.Learned(saved.BytesPerToken) // before the conversation is measured, so it is measured as it was
		a.Resume(messages)
		fmt.Fprintf(os.Stderr, "ykode: resumed %s (%d messages)\n", cfg.Session, len(messages))
	}
	// what actually goes out each turn: a tool that is not offered yet is not paid for
	fmt.Fprintf(os.Stderr, "ykode: %d tool specs, %d bytes in every request\n", len(a.Specs()), a.SpecBytes())
	if err := front.Run(a, cfg, ctxSize); err != nil {
		return err
	}
	fmt.Fprint(os.Stderr, resumeNote(cfg, a.SavedMessages()))
	return nil
}

// resumeNote tells a leaving session where the conversation went, so it can be picked up again:
// the file is written as you work, but nothing else says it is there.
func resumeNote(cfg config.Config, saved int) string {
	if saved == 0 {
		return ""
	}
	return fmt.Sprintf("ykode: %d messages saved in %s\nykode: resume with --continue %s\n", saved, cfg.SessionFile, cfg.Session)
}

// startupConfig resolves the settings and asks about an untrusted project's file; done means nothing is left to run.
func startupConfig(args []string, version string) (config.Config, bool, error) {
	cfg, err := config.Load(args)
	if errors.Is(err, flag.ErrHelp) {
		return cfg, true, nil
	}
	if err != nil {
		return cfg, true, err
	}
	if cfg.Version {
		fmt.Printf("ykode %s\n", version)
		return cfg, true, nil
	}
	// a project's settings file goes only so far on its own: say what it wanted, and ask
	trusted, err := cli.ReportHeld(cfg, os.Stdin, cfg.Prompt == "" && cli.IsTerminal(os.Stdin), os.Stderr)
	if err != nil || !trusted {
		return cfg, err != nil, err
	}
	cfg, err = config.Load(args)
	return cfg, err != nil, err
}

// askForServer saves the choice to settings.yaml; a flag or environment value still wins next time.
func askForServer(cfg config.Config, problem string) (config.Config, error) {
	res, err := tui.Setup(context.Background(), tui.SetupStart{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Problem: problem})
	if err != nil {
		return cfg, err
	}
	path, err := config.SaveEndpoint(res.BaseURL, res.APIKey, res.Model)
	if err != nil {
		return cfg, err
	}
	fmt.Fprintf(os.Stderr, "ykode: saved %s (%s) to %s\n", res.BaseURL, res.Model, path)
	cfg.BaseURL, cfg.Model, cfg.EndpointSet = res.BaseURL, res.Model, true
	if res.APIKey != "" {
		cfg.APIKey = res.APIKey
	}
	return cfg, nil
}

// connect finds the model and its window, and prints them unless the screen will show them. The note
// says what was decided for the user that they did not ask for, such as a model swapped for the one the
// server has.
func connect(cfg config.Config) (*llm.Client, llm.ModelInfo, int, string, error) {
	client := llm.NewClient(cfg.BaseURL, cfg.APIKey)
	client.Session = cfg.Session // so a proxy keeps one conversation on the slot holding its cache
	client.Idle = cfg.StreamIdle
	probeCtx, cancelProbe := context.WithTimeout(context.Background(), startupProbeTimeout)
	defer cancelProbe()

	model, note, err := pickModel(probeCtx, client, cfg)
	if err != nil {
		return nil, model, 0, "", err
	}
	ctxSize := cmp.Or(cfg.CtxSize, model.CtxSize)
	// the screen shows the model and the window in its status bar, so no banner to bury
	if !cfg.TUI {
		banner := os.Stdout
		if cfg.JSON {
			banner = os.Stderr
		}
		if note != "" {
			fmt.Fprintln(os.Stderr, "ykode:", note)
		}
		fmt.Fprintf(banner, "model: %s\nroot:  %s\nctx:   %s\n\n", model.ID, cfg.Root, describeCtx(ctxSize))
	}
	return client, model, ctxSize, note, nil
}

// pickModel resolves an empty model to the server's first. A model a settings file pinned that this
// server does not list is a leftover from another endpoint, so the server's own is used and the note
// says so; one named by --model or the environment is kept, since a proxy may load a model it does not
// list yet, and the note warns instead.
func pickModel(ctx context.Context, client *llm.Client, cfg config.Config) (llm.ModelInfo, string, error) {
	models, err := client.Models(ctx)
	if err != nil {
		return llm.ModelInfo{}, "", fmt.Errorf("cannot reach server at %s: %w", cfg.BaseURL, err)
	}
	if cfg.Model == "" {
		return models[0], "", nil
	}
	ids := make([]string, 0, len(models))
	for _, m := range models {
		if m.ID == cfg.Model {
			return m, "", nil
		}
		ids = append(ids, m.ID)
	}
	if cfg.ModelFromFile() {
		return models[0], fmt.Sprintf("settings.yaml pins model %q, which %s does not list: using %q instead (change it with /settings)",
			cfg.Model, cfg.BaseURL, models[0].ID), nil
	}
	return llm.ModelInfo{ID: cfg.Model}, fmt.Sprintf("%q is not listed by %s (models: %s)", cfg.Model, cfg.BaseURL, strings.Join(ids, ", ")), nil
}

func describeCtx(tokens int) string {
	if tokens <= 0 {
		return "unknown (set --ctx to budget responses)"
	}
	return strconv.Itoa(tokens) + " tokens"
}

// auditLogger writes the tool-call log unless a setting turned it off.
func auditLogger(cfg config.Config) *audit.Logger {
	if !cfg.LogAudit {
		return nil
	}
	return audit.New(cfg.AuditDir, cfg.Session)
}
