// Command eval runs scripted tasks against a real server, so the prompt is tuned by numbers, not impressions.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/home"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// options are the eval's own flags; everything else on the command line is ykode's.
type options struct {
	only, baseline        string
	keep, verbose, useSet bool
	repeat                int
}

func main() {
	opts, ykodeArgs, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(2)
	}
	evalsDir := filepath.Join(home.Dir(), "evals") // the real home, before settings are isolated below
	if !opts.useSet {
		isolateSettings()
	}
	cfg, err := config.Load(ykodeArgs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(2)
	}

	ctx := context.Background()
	client := llm.NewClient(cfg.BaseURL)
	client.Idle = cfg.StreamIdle
	model, err := evalModel(ctx, client, cfg.Model)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(2)
	}
	fmt.Printf("eval: %s at %s, context %d, %d run(s) per task\n\n", model.ID, cfg.BaseURL, model.CtxSize, opts.repeat)

	run := newRunRecord(model.ID, cfg)
	for _, t := range tasks() {
		if opts.only != "" && !strings.Contains(t.name, opts.only) {
			continue
		}
		if t.vision && !model.Vision {
			fmt.Printf("skip %-24s %s reads no pictures\n", t.name, model.ID)
			continue
		}
		var results []evalResult
		for range opts.repeat {
			res := runTask(ctx, client, model, cfg, t)
			results = append(results, res)
			if opts.verbose || !res.passed {
				fmt.Printf("     %s: %s\n", res.name, res.note)
				for _, line := range strings.Split(strings.TrimRight(res.story, "\n"), "\n") {
					fmt.Printf("      | %s\n", line)
				}
			}
			if !opts.keep {
				_ = os.RemoveAll(res.dir) // a leftover temp folder is the system's to clear, not a failed run
			}
		}
		rec := summarise(t.name, results)
		run.Tasks = append(run.Tasks, rec)
		fmt.Println(rec.line())
	}

	path, err := run.save(evalsDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
	} else {
		fmt.Printf("\nsaved %s\n", path)
	}
	if opts.baseline != "" {
		base, err := loadRun(evalsDir, opts.baseline, path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "eval:", err)
			os.Exit(2)
		}
		fmt.Print("\n" + compare(base, run))
	}
	if !run.allPassed() {
		os.Exit(1)
	}
}

// parseArgs takes the eval's flags out and leaves ykode's in order, so ARGS needs no -- between them
func parseArgs(args []string) (options, []string, error) {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	var o options
	fs.StringVar(&o.only, "only", "", "run only tasks whose name contains this")
	fs.StringVar(&o.baseline, "baseline", "", "compare with a saved run: a path, or last")
	fs.BoolVar(&o.keep, "keep", false, "keep the fixture directories and print their paths")
	fs.BoolVar(&o.verbose, "verbose", false, "print what the model did for every run, not only the failures")
	fs.BoolVar(&o.useSet, "use-settings", false, "apply ~/.ykode/settings.yaml instead of built-in defaults")
	fs.IntVar(&o.repeat, "repeat", 3, "runs per task; one run at temperature 0.7 is not a measurement")

	var own, rest []string
	for i := 0; i < len(args); i++ {
		name, _, hasValue := strings.Cut(strings.TrimLeft(args[i], "-"), "=")
		f := fs.Lookup(name)
		if f == nil || !strings.HasPrefix(args[i], "-") {
			rest = append(rest, args[i])
			continue
		}
		own = append(own, args[i])
		if _, isBool := f.Value.(interface{ IsBoolFlag() bool }); !hasValue && !isBool && i+1 < len(args) {
			i++
			own = append(own, args[i])
		}
	}
	if err := fs.Parse(own); err != nil {
		return o, nil, err
	}
	if o.repeat < 1 {
		return o, nil, fmt.Errorf("--repeat must be at least 1, got %d", o.repeat)
	}
	return o, rest, nil
}

// isolateSettings points ykode at an empty home, so the user's settings and AGENTS.md change no measurement
func isolateSettings() {
	dir, err := os.MkdirTemp("", "ykode-eval-home-")
	if err != nil {
		return
	}
	if os.Setenv("YKODE_HOME", dir) == nil {
		home.Reset()
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
