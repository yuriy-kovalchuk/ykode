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
	cfg, done, err := startupConfig(args, version)
	if done || err != nil {
		return err
	}
	// piped in or out, a screen has nowhere to draw
	cfg.TUI = cfg.TUI && cli.IsTerminal(os.Stdin) && cli.IsTerminal(os.Stdout)
	if !cfg.TUI && cfg.Prompt == "" {
		return errors.New("ykode needs a terminal; use -p TASK to answer one task without one")
	}
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
	// releasing the handle cannot fail in a way this run could act on, so the error is named as discarded
	defer func() { _ = root.Close() }()

	instructions, err := agent.LoadInstructions(home.Dir(), cfg.Root, agent.InstructionBytes(ctxSize))
	if err != nil {
		fmt.Fprintln(os.Stderr, "ykode:", err, "- continuing without them")
	}
	toolOpts, agentOpts := Options(cfg, model.ID, ctxSize)
	agentOpts.Instructions = instructions
	agentOpts.Session = session.New(cfg.SessionDir, cfg.Session, cfg.Root)
	agentOpts.Vision = model.Vision // what this server's model takes as input, from the server itself

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
	fmt.Fprint(os.Stderr, resumeNote(a.SessionFile(), cfg.Root, a.SavedMessages()))
	return nil
}

// resumeNote tells a leaving session where the conversation went, spelled out to be pasted as it stands
func resumeNote(file, root string, saved int) string {
	if saved == 0 || file == "" {
		return ""
	}
	// --root belongs in the line: a saved conversation is read back only by the project it was written in
	if strings.ContainsAny(root, ` "'`) {
		root = strconv.Quote(root) // a pasted command breaks on a path with a space
	}
	return fmt.Sprintf("ykode: %d messages saved in %s\nykode: come back with: ykode --continue %s --root %s\n",
		saved, file, session.ID(file), root)
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

// askForServer saves the choice to this project's settings file; a flag or environment value still wins
func askForServer(cfg config.Config, problem string) (config.Config, error) {
	res, err := tui.Setup(context.Background(), tui.SetupStart{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Problem: problem})
	if err != nil {
		return cfg, err
	}
	path, local, err := config.SaveEndpoint(cfg.Root, res.BaseURL, res.APIKey, res.Model)
	if err != nil {
		return cfg, err
	}
	where := path
	if !local {
		where = path + " (the global file: this project has no .ykode/settings.yaml)"
	}
	fmt.Fprintf(os.Stderr, "ykode: saved %s (%s) to %s\n", res.BaseURL, res.Model, where)
	cfg.BaseURL, cfg.Model, cfg.EndpointSet = res.BaseURL, res.Model, true
	if res.APIKey != "" {
		cfg.APIKey = res.APIKey
	}
	return cfg, nil
}

// connect finds the model and its window, printing what was decided for the user without being asked
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
		if note != "" {
			fmt.Fprintln(os.Stderr, "ykode:", note)
		}
		fmt.Printf("model: %s\nroot:  %s\nctx:   %s\n\n", model.ID, cfg.Root, describeCtx(ctxSize))
	}
	return client, model, ctxSize, note, nil
}

// pickModel resolves an empty model to the server's first, keeping one named by --model or the environment
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
