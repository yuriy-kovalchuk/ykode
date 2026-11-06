package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/config"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// defaultMaxTurns bounds a task: anything longer has usually stopped looping.
const defaultMaxTurns = 8

// transcript is what one run left behind.
type transcript struct {
	tools    []string
	failures []string
	results  []string // what each tool handed back, one-lined
	notice   []string
	answer   string // text of the last turn that answered in prose
}

// contains asks about the prose the model finished with.
func (t *transcript) contains(needle string) bool { return containsFold(t.answer, needle) }

// sawDiff checks that an edit came back with a diff body.
func (t *transcript) sawDiff() bool {
	for _, result := range t.results {
		if strings.Contains(result, "@@") {
			return true
		}
	}
	return false
}

func (t *transcript) ranTool(name string) bool {
	for _, tool := range t.tools {
		if tool == name {
			return true
		}
	}
	return false
}

// describe is what the model did, step by step, for tuning runs and failures.
func (t *transcript) describe() string {
	var b strings.Builder
	for i, tool := range t.tools {
		fmt.Fprintf(&b, "%d. %s\n", i+1, tool)
		if i < len(t.results) && t.results[i] != "" {
			fmt.Fprintf(&b, "   -> %s\n", flat(t.results[i], 160))
		}
	}
	for _, why := range t.failures {
		fmt.Fprintf(&b, "FAILED %s\n", flat(why, 160))
	}
	for _, note := range t.notice {
		fmt.Fprintf(&b, "NOTICE %s\n", flat(note, 160))
	}
	if t.answer != "" {
		b.WriteString("answer: " + flat(t.answer, 240))
	}
	return strings.TrimRight(b.String(), "\n")
}

// flat squashes a multi-line result so one transcript line stays scannable.
func flat(s string, maxChars int) string {
	s = strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
	if len(s) > maxChars {
		return s[:maxChars] + "…"
	}
	return s
}

func (t *transcript) failedWith(needle string) bool {
	for _, why := range t.failures {
		if containsFold(why, needle) {
			return true
		}
	}
	return false
}

// recorder is the eval's Sink: it records the stream and answers nothing outside the defaults.
type recorder struct {
	transcript
	events       []agent.Event
	answerByTurn map[int]string
}

func newRecorder() *recorder {
	return &recorder{answerByTurn: map[int]string{}}
}

func (r *recorder) Emit(ev agent.Event) agent.Decision {
	r.events = append(r.events, ev)
	switch ev.Kind {
	case agent.Answer:
		r.answerByTurn[ev.Turn] += ev.Text
	case agent.ToolStart:
		r.tools = append(r.tools, ev.Tool)
	case agent.ToolFail:
		r.failures = append(r.failures, ev.Text)
	case agent.ToolEnd:
		r.results = append(r.results, ev.Text)
	case agent.Notice:
		r.notice = append(r.notice, ev.Text)
	}
	return agent.Refuse
}

// lastAnswer is the prose of the highest turn that answered, which is the answer to the task.
func (r *recorder) lastAnswer() string {
	best, text := 0, ""
	for turn, body := range r.answerByTurn {
		if turn > best {
			best, text = turn, body
		}
	}
	r.answer = text
	return text
}

// evalResult is one task's outcome.
type evalResult struct {
	name      string
	passed    bool
	turns     int
	prompt    int
	generated int
	seconds   float64
	note      string
	story     string
	dir       string
}

func runTask(ctx context.Context, client *llm.Client, model llm.ModelInfo, cfg config.Config, t task) evalResult {
	base, err := os.MkdirTemp("", "ykode-eval-")
	if err != nil {
		return evalResult{name: t.name, note: err.Error()}
	}
	project := filepath.Join(base, "project")
	if err := writeFixture(base, t.fixture, t.outside); err != nil {
		return evalResult{name: t.name, note: err.Error(), dir: base}
	}
	root, err := os.OpenRoot(project)
	if err != nil {
		return evalResult{name: t.name, note: err.Error(), dir: base}
	}
	defer root.Close()

	store := tools.NewResultStore(filepath.Join(base, "results"))
	resultBytes := tools.ResultBytesFor(model.CtxSize)
	instructions, _ := agent.LoadInstructions(project)
	rec := newRecorder()
	maxTurns := t.maxTurns
	if maxTurns == 0 {
		maxTurns = defaultMaxTurns
	}
	a := agent.New(client, registryFor(root, project, cfg, resultBytes), model.ID, agent.Options{
		MaxTurns:     maxTurns,
		MaxTokens:    cfg.MaxTokens,
		CtxSize:      model.CtxSize,
		Think:        cfg.Think,
		AssumeYes:    true, // the harness has nobody to ask, so in-project work runs and outside work is refused
		Instructions: instructions,
		MaxResult:    resultBytes,
		Store:        store,
	}, rec)

	start := time.Now()
	askErr := a.Ask(ctx, t.prompt)
	usage := a.Usage()
	rec.lastAnswer()

	passed, note := t.check(project, &rec.transcript, askErr)
	if askErr != nil && passed {
		note = askErr.Error() + "; " + note
	}
	return evalResult{
		name:      t.name,
		passed:    passed,
		turns:     usage.Turns,
		prompt:    usage.PromptTok,
		generated: usage.GenTok,
		seconds:   time.Since(start).Seconds(),
		note:      note,
		story:     rec.transcript.describe(),
		dir:       base,
	}
}

func registryFor(root *os.Root, project string, cfg config.Config, resultBytes int) *tools.Registry {
	return tools.BuiltIn(root, project, tools.Options{
		ReadLines:      cfg.ReadLines,
		SearchLimit:    cfg.SearchLimit,
		ExecTimeout:    cfg.ExecTimeout,
		CommandLogDir:  cfg.CommandLogDir,
		MaxResultBytes: resultBytes,
	})
}

// writeFixture lays out <base>/project for the agent's root plus anything one level above it.
func writeFixture(base string, files, outside map[string]string) error {
	for name, body := range files {
		if err := writeFile(filepath.Join(base, "project", name), body); err != nil {
			return err
		}
	}
	for name, body := range outside {
		if err := writeFile(filepath.Join(base, name), body); err != nil {
			return err
		}
	}
	return nil
}

func writeFile(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("make %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func readBack(project, name string) string {
	raw, err := os.ReadFile(filepath.Join(project, name))
	if err != nil {
		return ""
	}
	return string(raw)
}

// containsFold is how every check reads the model's prose: case never matters here.
func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
