// Command ykode is a terminal coding agent for an OpenAI-compatible server such as llama-server.
package main

import (
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
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

const (
	startupProbeTimeout = 10 * time.Second
	spillLogAge         = 24 * time.Hour // how long a saved command log stays worth keeping
)

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

	// command output logs are written one per oversized command and nothing else deletes them
	if n, err := tools.PruneSpillLogs(os.TempDir(), spillLogAge); err != nil {
		fmt.Fprintln(os.Stderr, "ykode:", err)
	} else if n > 0 {
		fmt.Fprintf(os.Stderr, "ykode: removed %d old command log(s)\n", n)
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
	fmt.Printf("model: %s\nroot:  %s\nctx:   %s\n\n", model.ID, cfg.Root, describeCtx(ctxSize))

	root, err := os.OpenRoot(cfg.Root)
	if err != nil {
		return fmt.Errorf("open root %q: %w", cfg.Root, err)
	}
	defer root.Close()

	registry := tools.BuiltIn(root, cfg.Root)

	lines := cli.NewLineSource(os.Stdin)
	confirm := cli.NewConfirmer(lines, os.Stdout)
	renderer := cli.NewRenderer(os.Stdout, os.Stderr)
	a := agent.New(client, registry, model.ID, agent.Options{
		MaxTurns:      cfg.MaxTurns,
		MaxTokens:     cfg.MaxTokens,
		CtxSize:       ctxSize,
		Think:         cfg.Think,
		AssumeYes:     cfg.AssumeYes,
		NoVerify:      cfg.NoVerify,
		SendReasoning: cfg.SendReasoning,
		Confirm:       confirm.Ask,
	}, renderer, os.Stderr)

	return cli.NewREPL(a, lines, os.Stdout, os.Stderr).Run(context.Background())
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
