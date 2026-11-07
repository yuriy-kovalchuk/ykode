package agent

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestTheNewPromptEndsWithTheSessionsEnvironment(t *testing.T) {
	root := t.TempDir() // not a git repository
	a := New(nil, nil, "m", Options{NewPrompt: true, Root: root, Instructions: "<project_instructions>be terse</project_instructions>"}, nil)
	prompt := a.history[0].Content

	for _, want := range []string{
		"<workflow>", "project root: " + root, "os: " + runtime.GOOS + "/" + runtime.GOARCH,
		"date: " + time.Now().Format("2006-01-02"), "git: not a git repository", "be terse",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("new prompt misses %q", want)
		}
	}
	if strings.Index(prompt, "<rules>") > strings.Index(prompt, "<environment>") {
		t.Error("the per-session environment comes before the fixed rules, so no two sessions share a cached prefix")
	}
	if strings.Contains(prompt, "{{") {
		t.Error("a placeholder was left unfilled")
	}
}

func TestEveryPromptSaysWhichSystemItRunsOn(t *testing.T) {
	prompt := New(nil, nil, "m", Options{Root: t.TempDir()}, nil).history[0].Content
	if !strings.HasPrefix(prompt, SystemPrompt) {
		t.Error("the default prompt no longer starts with SystemPrompt, so its cached prefix moved")
	}
	if !strings.Contains(prompt, "os: "+runtime.GOOS+"/"+runtime.GOARCH) {
		t.Errorf("the default prompt does not name the OS:\n%s", prompt)
	}
}

func TestResetKeepsTheSamePrompt(t *testing.T) {
	a := New(nil, nil, "m", Options{NewPrompt: true, Root: t.TempDir()}, nil)
	before := a.history[0].Content
	a.Reset()
	if a.history[0].Content != before {
		t.Error("the system message was rebuilt by /reset")
	}
}
