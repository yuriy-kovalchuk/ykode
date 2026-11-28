package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/config"
)

// runRecord is one eval run as saved to ~/.ykode/evals, so a later run can be compared with it.
type runRecord struct {
	Time     time.Time         `json:"time"`
	Model    string            `json:"model"`
	BaseURL  string            `json:"base_url"`
	Commit   string            `json:"commit"`
	Settings map[string]string `json:"settings"` // what changes behaviour, as it was resolved
	Tasks    []taskRecord      `json:"tasks"`
}

// taskRecord is one task over all its runs: medians for the costs, totals for the counters.
type taskRecord struct {
	Name      string         `json:"name"`
	Passed    int            `json:"passed"`
	Runs      int            `json:"runs"`
	Turns     int            `json:"turns"`
	Prompt    int            `json:"prompt"`
	Generated int            `json:"generated"`
	Seconds   float64        `json:"seconds"`
	Counters  map[string]int `json:"counters,omitempty"`
}

func newRunRecord(model string, cfg config.Config) runRecord {
	return runRecord{
		Time: time.Now(), Model: model, BaseURL: cfg.BaseURL, Commit: gitCommit(),
		Settings: map[string]string{
			"thinking_level":  cfg.ThinkingLevel,
			"thinking_budget": fmt.Sprint(cfg.ThinkingBudget),
			"max_tokens":      fmt.Sprint(cfg.MaxTokens),
			"read_lines":      fmt.Sprint(cfg.ReadLines),
			"safety_margin":   fmt.Sprint(cfg.SafetyMargin),
			"disabled_tools":  strings.Join(cfg.DisabledTools, ","),
		},
	}
}

// gitCommit names the code under test, with -dirty when the tree has changes.
func gitCommit() string {
	out, err := exec.Command("git", "describe", "--always", "--dirty").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func summarise(name string, results []evalResult) taskRecord {
	rec := taskRecord{Name: name, Runs: len(results), Counters: map[string]int{}}
	var turns, prompt, generated []int
	var seconds []float64
	for _, r := range results {
		if r.passed {
			rec.Passed++
		}
		turns, prompt, generated = append(turns, r.turns), append(prompt, r.prompt), append(generated, r.generated)
		seconds = append(seconds, r.seconds)
		for k, v := range r.counters {
			rec.Counters[k] += v
		}
	}
	rec.Turns, rec.Prompt, rec.Generated, rec.Seconds = median(turns), median(prompt), median(generated), median(seconds)
	return rec
}

func median[T int | float64](values []T) T {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return sorted[len(sorted)/2]
}

// line is the one-line report of a task: pass rate, median costs, and whatever the counters saw.
func (t taskRecord) line() string {
	status := "pass"
	if t.Passed < t.Runs {
		status = "FAIL"
	}
	return fmt.Sprintf("%-4s %-24s %d/%d  %2d turns %7d prompt %6d generated %6.0fs  %s",
		status, t.Name, t.Passed, t.Runs, t.Turns, t.Prompt, t.Generated, t.Seconds, t.counterText())
}

func (t taskRecord) counterText() string {
	names := make([]string, 0, len(t.Counters))
	for k, v := range t.Counters {
		if v > 0 {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, k := range names {
		parts[i] = fmt.Sprintf("%s=%d", k, t.Counters[k])
	}
	return strings.Join(parts, " ")
}

func (r runRecord) allPassed() bool {
	for _, t := range r.Tasks {
		if t.Passed < t.Runs {
			return false
		}
	}
	return true
}

func (r runRecord) save(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, r.Time.Format("20060102-150405")+".json")
	return path, os.WriteFile(path, raw, 0o600)
}

// loadRun reads a saved run: a path, or "last" for the newest one other than the run just saved.
func loadRun(dir, which, current string) (runRecord, error) {
	path := which
	if which == "last" {
		files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
		files = slices.DeleteFunc(files, func(f string) bool { return f == current })
		if len(files) == 0 {
			return runRecord{}, errors.New("--baseline last: no earlier run saved in " + dir)
		}
		path = files[len(files)-1]
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return runRecord{}, err
	}
	var r runRecord
	return r, json.Unmarshal(raw, &r)
}

// compare lists every task in both runs with what moved; a lower pass rate is a regression.
func compare(base, now runRecord) string {
	var b strings.Builder
	fmt.Fprintf(&b, "against %s (%s, %s):\n", base.Time.Format("2006-01-02 15:04"), base.Model, base.Commit)
	before := map[string]taskRecord{}
	for _, t := range base.Tasks {
		before[t.Name] = t
	}
	for _, t := range now.Tasks {
		old, ok := before[t.Name]
		if !ok {
			fmt.Fprintf(&b, "  new        %-24s %d/%d\n", t.Name, t.Passed, t.Runs)
			continue
		}
		mark := "  same      "
		switch rate, oldRate := float64(t.Passed)/float64(t.Runs), float64(old.Passed)/float64(old.Runs); {
		case rate < oldRate:
			mark = "  REGRESSION"
		case rate > oldRate:
			mark = "  better    "
		}
		fmt.Fprintf(&b, "%s %-24s %d/%d -> %d/%d  turns %d -> %d  prompt %d -> %d\n",
			mark, t.Name, old.Passed, old.Runs, t.Passed, t.Runs, old.Turns, t.Turns, old.Prompt, t.Prompt)
	}
	return b.String()
}
