package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/session"
)

func TestThePromptEndsWithTheSessionsEnvironment(t *testing.T) {
	root := t.TempDir() // not a git repository
	a := New(nil, nil, "m", Options{Root: root, Instructions: "<project_instructions>be terse</project_instructions>"}, nil)
	prompt := a.history[0].Content

	for _, want := range []string{
		"Guidelines:", "project root: " + root, "working directory: " + root, "needs no cd", "os: " + runtime.GOOS + "/" + runtime.GOARCH,
		"be terse",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt misses %q", want)
		}
	}
	if strings.Index(prompt, "Guidelines:") > strings.Index(prompt, "<environment>") {
		t.Error("the per-session environment comes before the fixed text, so no two sessions share a cached prefix")
	}
	if strings.Contains(prompt, "{{") {
		t.Error("a placeholder was left unfilled")
	}
	// what changes between runs is not in the prompt: it would move the cached prefix, and with it the
	// tool definitions and the whole conversation, whenever a commit or a new day changed it
	for _, volatile := range []string{"date:", "git:", time.Now().Format("2006-01-02")} {
		if strings.Contains(prompt, volatile) {
			t.Errorf("the prompt carries %q, which changes between runs and would move the cached prefix", volatile)
		}
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
	a := New(nil, nil, "m", Options{Root: t.TempDir()}, nil)
	before := a.history[0].Content
	a.Reset()
	if a.history[0].Content != before {
		t.Error("the system message was rebuilt by /reset")
	}
}

// the footer asks for the branch each prompt, so a command that changed branch shows up
// gitRepo is a project with one commit on a branch of its own.
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	git("-c", "user.email=ykode@example.com", "-c", "user.name=ykode", "commit", "-q", "--allow-empty", "-m", "start")
	git("checkout", "-q", "-b", "footer")
	return dir
}

func TestGitBranchNamesWhatIsCheckedOut(t *testing.T) {
	dir := gitRepo(t)
	if got := GitBranch(dir); got != "footer" {
		t.Errorf("GitBranch = %q, want footer", got)
	}
	if got := GitBranch(t.TempDir()); got != "" {
		t.Errorf("GitBranch of a directory with no repository = %q, want empty", got)
	}
}

// the branch the prompt was built on is kept, so the first frame does not wait for git a third time
func TestTheAgentKeepsTheBranchItsPromptWasBuiltOn(t *testing.T) {
	dir := gitRepo(t)
	a := New(nil, nil, "m", Options{Root: dir}, nil)
	if a.Branch() != "footer" {
		t.Errorf("the agent knows branch %q, want footer", a.Branch())
	}
	if prompt := a.history[0].Content; strings.Contains(prompt, "footer") {
		t.Errorf("the prompt names the branch, which a checkout would change:\n%s", prompt)
	}
}

// a change in the tree is said, and the branch is still the branch
func TestThePromptSaysWhenTheTreeHasChanges(t *testing.T) {
	dir := gitRepo(t)
	if err := os.WriteFile(dir+"/note.txt", []byte("work"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, branch := gitState(dir)
	if state != "branch footer, uncommitted changes" || branch != "footer" {
		t.Errorf("gitState = %q, %q", state, branch)
	}
}

// an ask ends with the date and the state of the tree, and again only when one of them changed
func TestAnAskCarriesTheDateAndTheTreeStateOnlyWhenTheyChange(t *testing.T) {
	dir := gitRepo(t)
	stub := &rawStub{scripts: []string{sse("", "ok", "", nil, "stop")}}
	log := session.New(t.TempDir(), "s1", dir)
	a, done := stub.agent(t, dir, log)
	defer done()
	a.opts.NoVerify = true
	ask := func(text string) string {
		t.Helper()
		if err := a.Ask(context.Background(), text); err != nil {
			t.Fatal(err)
		}
		for i := len(a.history) - 1; i >= 0; i-- {
			if a.history[i].Role == "user" {
				return a.history[i].Content
			}
		}
		return ""
	}

	first := ask("first")
	text, note := session.SplitEnvironment(first)
	if text != "first" || !strings.Contains(note, "date: "+time.Now().Format("2006-01-02")) || !strings.Contains(note, "git: branch footer, clean") {
		t.Errorf("the first ask is %q: want the ask, then the date and a clean tree", first)
	}
	if again := ask("second"); again != "second" {
		t.Errorf("an ask that changed nothing repeated the note: %q", again)
	}
	if err := os.WriteFile(dir+"/work.txt", []byte("edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, note := session.SplitEnvironment(ask("third")); !strings.Contains(note, "uncommitted changes") {
		t.Errorf("a tree that changed was not said: %q", note)
	}
	a.Reset()
	if _, note := session.SplitEnvironment(ask("after a reset")); note == "" {
		t.Error("a new conversation starts without the note")
	}
}

// a resume after the tree changed repeats the earlier requests exactly: only the new ask differs, and the
// note on it says what changed. This is what keeps the server's cache of a long conversation alive
func TestAResumeAfterTheTreeChangedKeepsEverythingBeforeTheNewAsk(t *testing.T) {
	dir := gitRepo(t)
	home := t.TempDir()
	log := session.New(home, "s1", dir)
	first := &rawStub{scripts: []string{sse("", "five lines", "", nil, "stop")}}
	a, done := first.agent(t, dir, log)
	defer done()
	a.opts.NoVerify = true
	if err := a.Ask(context.Background(), "how big is this?"); err != nil {
		t.Fatal(err)
	}
	before := shape(t, first.bodies[0])

	if err := os.WriteFile(dir+"/work.txt", []byte("edit"), 0o600); err != nil { // what an edit session does to the tree
		t.Fatal(err)
	}
	back, err := session.Load(filepath.Join(home, "s1.jsonl"), dir)
	if err != nil {
		t.Fatal(err)
	}
	second := &rawStub{scripts: []string{sse("", "ok", "", nil, "stop")}}
	b, done2 := second.agent(t, dir, nil)
	defer done2()
	b.opts.NoVerify = true
	b.Resume(back)
	if err := b.Ask(context.Background(), "and now?"); err != nil {
		t.Fatal(err)
	}
	after := shape(t, second.bodies[0])

	if string(before.Tools) != string(after.Tools) {
		t.Error("the tool definitions moved")
	}
	for i := range before.Messages { // the system prompt, and the first ask with its note
		if string(before.Messages[i]) != string(after.Messages[i]) {
			t.Fatalf("message %d moved, so the server would read the conversation again:\n before: %.300s\n after:  %.300s",
				i, before.Messages[i], after.Messages[i])
		}
	}
	if last := string(after.Messages[len(after.Messages)-1]); !strings.Contains(last, "uncommitted changes") {
		t.Errorf("the new ask does not say the tree changed: %s", last)
	}
}
