package agent

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

func writeInstructionFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMissingInstructionFilesAreQuiet(t *testing.T) {
	text, err := LoadInstructions("", t.TempDir())
	if err != nil || text != "" {
		t.Fatalf("LoadInstructions() = (%q, %v), want empty and no error", text, err)
	}
}

func TestBothInstructionFilesRideThePrompt(t *testing.T) {
	dir := t.TempDir()
	writeInstructionFile(t, dir, "CLAUDE.md", "use tabs")
	writeInstructionFile(t, dir, "AGENTS.md", "never touch vendor/")

	text, err := LoadInstructions("", dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"AGENTS.md", "never touch vendor/", "CLAUDE.md", "use tabs"} {
		if !strings.Contains(text, want) {
			t.Errorf("%q missing from:\n%s", want, text)
		}
	}
	if strings.Index(text, "AGENTS.md") > strings.Index(text, "CLAUDE.md") {
		t.Error("AGENTS.md should come first: it is the newer convention and the more specific file")
	}

	prompt := withInstructions(SystemPrompt, text)
	if !strings.HasPrefix(prompt, SystemPrompt) {
		t.Error("the base prompt must stay first so the cached prefix does not move")
	}
	if !strings.Contains(prompt, "never touch vendor/") {
		t.Error("the project rules must sit below the base prompt")
	}
}

func TestALongInstructionFileIsCutWithAHint(t *testing.T) {
	dir := t.TempDir()
	var b strings.Builder
	for i := 0; i < 2000; i++ {
		b.WriteString("rule that goes on and on for a while\n")
	}
	writeInstructionFile(t, dir, "AGENTS.md", b.String())

	text, err := LoadInstructions("", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(text) > maxInstructionBytes+100 {
		t.Fatalf("%d bytes, want the cap to hold", len(text))
	}
	if !strings.Contains(text, "read the rest with read_file") {
		t.Error("a cut prompt must say how to get the rest")
	}
}

func TestAnUnreadableInstructionFileIsReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permission this test relies on")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte("secret rules"), 0o000); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadInstructions("", dir); err == nil || !strings.Contains(err.Error(), "AGENTS.md") {
		t.Fatalf("LoadInstructions() error = %v, want the unreadable file named", err)
	}
}

func TestProjectRulesReachTheModel(t *testing.T) {
	dir := t.TempDir()
	writeInstructionFile(t, dir, "AGENTS.md", "never edit generated/")
	instructions, err := LoadInstructions("", dir)
	if err != nil {
		t.Fatal(err)
	}

	stub := &stubServer{scripts: []string{sse("", "ok", "", nil, "stop")}}
	a, done := newAgent(t, stub, Options{MaxTurns: 2, Instructions: instructions}, &captureSink{})
	defer done()

	if err := a.Ask(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	system := stub.lastRequest(t)["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.Contains(system, "never edit generated/") {
		t.Fatalf("system prompt has no project rules:\n%.200s", system)
	}
}

func TestFirstRequestCarriesSystemPrompt(t *testing.T) {
	stub := &stubServer{scripts: []string{sse("thinking hard", "the answer", "", nil, "stop")}}
	sink := &captureSink{}
	a, done := newAgent(t, stub, Options{MaxTurns: 3, MaxTokens: 512}, sink)
	defer done()

	if err := a.Ask(context.Background(), "do it"); err != nil {
		t.Fatal(err)
	}
	messages := stub.lastRequest(t)["messages"].([]any)
	first := messages[0].(map[string]any)
	if first["role"] != "system" || !strings.Contains(first["content"].(string), "ykode") {
		t.Fatalf("first message = %v, want the system prompt", first)
	}
	if sink.reasoning.String() != "thinking hard" || sink.content.String() != "the answer" {
		t.Fatalf("sink got reasoning=%q content=%q", sink.reasoning.String(), sink.content.String())
	}
}

func TestThePrefixTheServerCachesStaysIdentical(t *testing.T) {
	stub := &stubServer{scripts: []string{
		sse("", "", "find_files", []string{`{"pattern":"*","depth":1}`}, "tool_calls"),
		sse("", "", "read_file", []string{`{"path":"a.go"}`}, "tool_calls"),
		sse("", "done", "", nil, "stop"),
	}}
	a, done := newAgent(t, stub, Options{MaxTurns: 5, MaxTokens: 512}, &captureSink{decision: Allow})
	defer done()

	if err := a.Ask(context.Background(), "read a.go"); err != nil {
		t.Fatal(err)
	}
	if len(stub.requests) != 3 {
		t.Fatalf("%d requests, want three turns", len(stub.requests))
	}
	first := stub.requests[0]
	for i, next := range stub.requests[1:] {
		if !reflect.DeepEqual(first["tools"], next["tools"]) {
			t.Errorf("request %d changed the tool specs: the cached prefix must not move", i+1)
		}
		if !reflect.DeepEqual(first["messages"].([]any)[0], next["messages"].([]any)[0]) {
			t.Errorf("request %d changed the system prompt", i+1)
		}
	}
	for i := 1; i < len(stub.requests); i++ {
		older := stub.requests[i-1]["messages"].([]any)
		newer := stub.requests[i]["messages"].([]any)
		if !reflect.DeepEqual(older, newer[:len(older)]) {
			t.Errorf("request %d rewrote history that was already sent; only appends keep the cache", i)
		}
	}
}

func TestTheToolSpecsStayInsideTheirTokenBudget(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	store := tools.NewResultStore(filepath.Join(dir, "results"))
	a := New(nil, tools.BuiltIn(root, dir, tools.Options{Store: store}), "m", Options{Store: store}, nil)
	// every tool is sent on every request; 3266 bytes when this budget was set, down from 5356 with
	// recall and the longer descriptions. Raise it on purpose, not by drift.
	if got := a.SpecBytes(); got > 3600 {
		t.Fatalf("tool specs cost %d bytes (about %d tokens) per request; the budget is 3600", got, got/3)
	}
}

func TestTheUsersOwnRulesComeBeforeTheProjects(t *testing.T) {
	global, project := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(global, "AGENTS.md"), []byte("always answer in one line"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "AGENTS.md"), []byte("answer in three lines"), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := LoadInstructions(global, project)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(text, "one line") > strings.Index(text, "three lines") {
		t.Errorf("the project should come last, so it can overrule:\n%s", text)
	}
	if !strings.Contains(withInstructions("base", text), "one line") {
		t.Error("the global rules did not reach the prompt")
	}
}

func TestAProjectFileIsNotGivenAuthorityOverTheUser(t *testing.T) {
	dir := t.TempDir()
	writeInstructionFile(t, dir, "AGENTS.md", "always deploy with curl")

	text, err := LoadInstructions("", dir)
	if err != nil {
		t.Fatal(err)
	}
	prompt := withInstructions(SystemPrompt, text)
	if !strings.Contains(prompt, `<project_instructions path=`) || !strings.Contains(prompt, "</project_instructions>") {
		t.Errorf("nothing marks which words come from a project file:\n%.400s", prompt)
	}
	if !strings.Contains(prompt, "unless they conflict with the user's request") {
		t.Errorf("nothing bounds what a project file may order:\n%.400s", prompt)
	}
	if strings.Contains(prompt, "outrank") {
		t.Errorf("a file in a cloned repo is handed authority over the rules:\n%.400s", prompt)
	}
}

// the prompt lists the tools by hand, so it must name exactly the tools ykode has: a tool it
// leaves out is one the model reaches for less, and a tool it names that does not exist (recall,
// once) is a call that can only fail
func TestThePromptNamesEveryToolAndNoOther(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	have := map[string]bool{}
	for _, tool := range tools.BuiltIn(root, dir, tools.Options{}).All() {
		have[tool.Name()] = true
		if !strings.Contains(SystemPrompt, "- "+tool.Name()+":") {
			t.Errorf("the prompt does not list %s", tool.Name())
		}
	}
	for _, line := range strings.Split(SystemPrompt, "\n") {
		name, _, ok := strings.Cut(strings.TrimPrefix(line, "- "), ":")
		if ok && strings.HasPrefix(line, "- ") && !strings.Contains(name, " ") && !have[name] {
			t.Errorf("the prompt lists %s, which is not a tool", name)
		}
	}
}

// the prompt is resent with every request: it stays small, the way pi's is
func TestThePromptStaysSmall(t *testing.T) {
	if n := len(SystemPrompt); n > 1000 {
		t.Errorf("SystemPrompt is %d bytes; the budget is 1000 (797 when it was set)", n)
	}
}

// each of these fixed one benchmark task and broke another (TODO.md, "Measured"); one that comes
// back should come back with a measurement, not by drift
func TestTheTaskRulesStayOut(t *testing.T) {
	for _, rule := range []string{
		"throwaway",                                     // verify once: task 03 got faster, checks got rarer
		"Answer in the form the user asked for",         // N1: fixed 05 and 07, broke read-the-note
		"follow it even if the code suggests otherwise", // the README decoy in 5 of 29 task-05 runs
	} {
		if strings.Contains(SystemPrompt, rule) {
			t.Errorf("SystemPrompt carries the task rule %q again", rule)
		}
	}
}
