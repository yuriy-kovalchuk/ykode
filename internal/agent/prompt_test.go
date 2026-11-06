package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeInstructionFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMissingInstructionFilesAreQuiet(t *testing.T) {
	text, err := LoadInstructions(t.TempDir())
	if err != nil || text != "" {
		t.Fatalf("LoadInstructions() = (%q, %v), want empty and no error", text, err)
	}
}

func TestBothInstructionFilesRideThePrompt(t *testing.T) {
	dir := t.TempDir()
	writeInstructionFile(t, dir, "CLAUDE.md", "use tabs")
	writeInstructionFile(t, dir, "AGENTS.md", "never touch vendor/")

	text, err := LoadInstructions(dir)
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

	text, err := LoadInstructions(dir)
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

	if _, err := LoadInstructions(dir); err == nil || !strings.Contains(err.Error(), "AGENTS.md") {
		t.Fatalf("LoadInstructions() error = %v, want the unreadable file named", err)
	}
}

func TestProjectRulesReachTheModel(t *testing.T) {
	dir := t.TempDir()
	writeInstructionFile(t, dir, "AGENTS.md", "never edit generated/")
	instructions, err := LoadInstructions(dir)
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
