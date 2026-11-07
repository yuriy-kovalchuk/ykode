// Command ykode is a terminal coding agent for an OpenAI-compatible server such as llama-server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
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
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/session"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
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
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if cfg.Audit {
		return showAudit(cfg)
	}

	if cfg.Version {
		fmt.Printf("ykode %s\n", version)
		return nil
	}

	// command output logs are written one per oversized command and nothing else deletes them
	if n, err := tools.PruneSpillLogs(cfg.CommandLogDir, cfg.KeepCommandLogs); err != nil {
		fmt.Fprintln(os.Stderr, "ykode:", err)
	} else if n > 0 {
		fmt.Fprintf(os.Stderr, "ykode: removed %d old command log(s)\n", n)
	}
	// a run's shortened tool results live here until they age out, and recall reads them back
	store := tools.NewResultStore(cfg.ResultDir)
	undo := &tools.UndoStack{}
	if n, err := tools.PruneOldDirs(filepath.Dir(cfg.ResultDir), "s", cfg.KeepCommandLogs); err != nil {
		fmt.Fprintln(os.Stderr, "ykode:", err)
	} else if n > 0 {
		fmt.Fprintf(os.Stderr, "ykode: removed %d old result dir(s)\n", n)
	}

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
	if cfg.JSON {
		fmt.Fprintf(os.Stderr, "model: %s\nroot:  %s\nctx:   %s\n\n", model.ID, cfg.Root, describeCtx(ctxSize))
	} else {
		fmt.Printf("model: %s\nroot:  %s\nctx:   %s\n\n", model.ID, cfg.Root, describeCtx(ctxSize))
	}

	root, err := os.OpenRoot(cfg.Root)
	if err != nil {
		return fmt.Errorf("open root %q: %w", cfg.Root, err)
	}
	defer root.Close()

	resultBytes := tools.ResultBytesFor(ctxSize)
	instructions, err := agent.LoadInstructions(cfg.Root)
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

	lines := cli.NewLineSource(os.Stdin)
	// -p with a pipe has nobody to answer a confirmation, so nothing outside the defaults may run
	var confirm *cli.Confirmer
	if cfg.Prompt == "" || cli.IsTerminal(os.Stdin) {
		confirm = cli.NewConfirmer(lines, os.Stdout)
	}

	var view agent.Sink = cli.NewView(os.Stdout, os.Stderr, confirm)
	var events *cli.JSONWriter
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
		Audit:         audit.New(cfg.AuditDir, cfg.Session),
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
	fmt.Fprintf(os.Stderr, "ykode: %d tool specs, %d bytes in every request\n", len(registry.All()), a.SpecBytes())

	if cfg.Prompt == "" {
		return cli.NewREPL(a, lines, os.Stdout, os.Stderr).Run(context.Background())
	}
	return answerOnce(a, events, cfg, ctxSize)
}

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

// showAudit prints the last run's tool calls and exits: the log outlives the conversation.
func showAudit(cfg config.Config) error {
	logger := audit.New(cfg.AuditDir, "")
	session := logger.LatestSession()
	if session == "" {
		return errors.New("no audit log yet; it is written to " + cfg.AuditDir)
	}
	entries, err := logger.Read(session)
	if err != nil {
		return err
	}
	fmt.Printf("ykode: run %s\n", session)
	fmt.Print(audit.Format(entries, "", auditLimit))
	return nil
}

// auditLimit caps how many calls one --audit listing shows.
const auditLimit = 25
