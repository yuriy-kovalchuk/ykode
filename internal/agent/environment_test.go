package agent

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestThePromptEndsWithTheSessionsEnvironment(t *testing.T) {
	root := t.TempDir() // not a git repository
	a := New(nil, nil, "m", Options{Root: root, Instructions: "<project_instructions>be terse</project_instructions>"}, nil)
	prompt := a.history[0].Content

	for _, want := range []string{
		"Guidelines:", "project root: " + root, "os: " + runtime.GOOS + "/" + runtime.GOARCH,
		"date: " + time.Now().Format("2006-01-02"), "git: not a git repository", "be terse",
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
	if prompt := a.history[0].Content; !strings.Contains(prompt, "git: branch footer, clean") {
		t.Errorf("the prompt does not say what is checked out:\n%s", prompt)
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
