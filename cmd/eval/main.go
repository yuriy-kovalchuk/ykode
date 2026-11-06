// Command eval runs scripted tasks against a real server and reports what the model actually did.
// It exists to tune the prompt and the tool descriptions with numbers instead of impressions.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

func main() {
	only := flag.String("only", "", "run only tasks whose name contains this")
	keep := flag.Bool("keep", false, "keep the fixture directories and print their paths")
	verbose := flag.Bool("verbose", false, "print what the model did for every task, not only the failures")
	flag.Parse()

	cfg, err := config.Load(flag.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(2)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := llm.NewClient(cfg.BaseURL, cfg.APIKey)
	model, err := evalModel(ctx, client, cfg.Model)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(2)
	}
	fmt.Printf("eval: %s at %s, context %d\n\n", model.ID, cfg.BaseURL, model.CtxSize)

	var passed, failed int
	for _, t := range tasks() {
		if *only != "" && !strings.Contains(t.name, *only) {
			continue
		}
		res := runTask(ctx, client, model, cfg, t)
		status := "pass"
		if !res.passed {
			status = "FAIL"
			failed++
		} else {
			passed++
		}
		fmt.Printf("%-4s %-20s %2d turns %7d prompt %6d generated %6.0fs  %s\n",
			status, res.name, res.turns, res.prompt, res.generated, res.seconds, res.note)
		if *verbose || !res.passed {
			for _, line := range strings.Split(strings.TrimRight(res.story, "\n"), "\n") {
				fmt.Printf("      | %s\n", line)
			}
		}
		if !*keep {
			os.RemoveAll(res.dir) // a failure is kept so the fixture can be inspected
		}
	}

	fmt.Printf("\n%d tasks, %d passed, %d failed\n", passed+failed, passed, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

// evalModel takes --model when given, else the first the server lists.
func evalModel(ctx context.Context, client *llm.Client, want string) (llm.ModelInfo, error) {
	probe, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	models, err := client.Models(probe)
	if err != nil {
		return llm.ModelInfo{}, err
	}
	if want == "" {
		return models[0], nil
	}
	for _, m := range models {
		if m.ID == want {
			return m, nil
		}
	}
	return llm.ModelInfo{}, fmt.Errorf("server does not list %q", want)
}
