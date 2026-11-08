package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setup(t *testing.T) (root *os.Root, dir string) {
	t.Helper()
	return treeFixture(t, map[string]string{"inside.txt": "inside\n"})
}

func TestResolveTarget(t *testing.T) {
	root := "/home/u/project"
	cases := []struct {
		raw      string
		resolved string
		outside  bool
	}{
		{"", "/home/u/project", false},
		{".", "/home/u/project", false},
		{"src/main.go", "/home/u/project/src/main.go", false},
		{"./src/main.go", "/home/u/project/src/main.go", false},
		{"src/../main.go", "/home/u/project/main.go", false},
		{"../notes.md", "/home/u/notes.md", true},
		{"src/../../notes.md", "/home/u/notes.md", true},
		{"/home/u/project/x", "/home/u/project/x", false},
		{"/etc/hosts", "/etc/hosts", true},
		{"..", "/home/u", true},
	}
	for _, c := range cases {
		got := resolveTarget(c.raw, root)
		if got.Resolved != c.resolved || got.Outside != c.outside {
			t.Errorf("%q: got (%s, outside=%v), want (%s, outside=%v)",
				c.raw, got.Resolved, got.Outside, c.resolved, c.outside)
		}
	}
}

func TestInsidePathNeedsNoApproval(t *testing.T) {
	root, dir := setup(t)
	read := NewReadFile(root, dir)
	targets, err := read.Targets(json.RawMessage(`{"path":"inside.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Outside {
		t.Fatalf("targets = %+v, want one in-project path", targets)
	}
	out, err := read.Run(context.Background(), json.RawMessage(`{"path":"inside.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "inside") {
		t.Fatalf("result = %q", out)
	}
}

func TestOutsidePathNeedsApprovalPerCall(t *testing.T) {
	root, dir := setup(t)
	// A file next to the project root, not inside it.
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "outside.txt"), []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	read := NewReadFile(root, dir)
	args := json.RawMessage(`{"path":"../outside.txt"}`)

	targets, err := read.Targets(args)
	if err != nil {
		t.Fatal(err)
	}
	if !targets[0].Outside {
		t.Fatal("expected the path to be flagged outside the project root")
	}

	if _, err := read.Run(context.Background(), args); err == nil ||
		!strings.Contains(err.Error(), "not approved") {
		t.Fatalf("unapproved read: err = %v, want a 'not approved' error", err)
	}

	approved := WithApprovals(context.Background(), []string{targets[0].Resolved})
	out, err := read.Run(approved, args)
	if err != nil {
		t.Fatalf("approved read: %v", err)
	}
	if !strings.Contains(out, "outside") {
		t.Fatalf("result = %q", out)
	}

	// Approval does not carry over: a fresh context must fail again.
	if _, err := read.Run(context.Background(), args); err == nil {
		t.Fatal("approval leaked into another call")
	}
}

func TestOutsideWriteCanCreateMissingDirs(t *testing.T) {
	root, dir := setup(t)
	write := NewWriteFile(root, dir)
	target := filepath.Join(filepath.Dir(dir), "newdir", "deep", "note.md")
	args, err := json.Marshal(map[string]string{"path": "../newdir/deep/note.md", "content": "written\n"})
	if err != nil {
		t.Fatal(err)
	}
	targets, err := write.Targets(args)
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithApprovals(context.Background(), []string{targets[0].Resolved})
	if _, err := write.Run(ctx, args); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "written\n" {
		t.Fatalf("file contents = %q", data)
	}
}

func TestApprovedOutsidePathCannotReachFurther(t *testing.T) {
	root, dir := setup(t)
	outsideDir := filepath.Join(filepath.Dir(dir), "shared")
	if err := os.MkdirAll(outsideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(filepath.Dir(dir), "secret.txt"), filepath.Join(outsideDir, "link")); err != nil {
		t.Fatal(err)
	}
	write := NewWriteFile(root, dir)
	args, _ := json.Marshal(map[string]string{"path": "../shared/link", "content": "oops\n"})
	targets, err := write.Targets(args)
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithApprovals(context.Background(), []string{targets[0].Resolved})
	if _, err := write.Run(ctx, args); err == nil {
		t.Fatal("an approved directory let a symlink write outside it")
	}
}

func TestInProjectSymlinkCannotEscape(t *testing.T) {
	root, dir := setup(t)
	if err := os.Symlink(filepath.Join(filepath.Dir(dir), "secret.txt"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	read := NewReadFile(root, dir)
	if _, err := read.Run(context.Background(), json.RawMessage(`{"path":"link"}`)); err == nil {
		t.Fatal("a symlink inside the root pointed outside it and the read succeeded")
	}
}

func TestFileResultsShowPathsTheModelCanReuse(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{"src/a.txt": "one\ntwo\n"})
	edited := runTool(t, NewEditFile(root, dir), `{"path":"src/a.txt","edits":[{"old_text":"two","new_text":"THREE"}]}`)
	wrote := runTool(t, NewWriteFile(root, dir), `{"path":"src/a.txt","content":"four\n"}`)
	for _, got := range []string{edited, wrote} {
		if !strings.Contains(got, "src/a.txt") {
			t.Errorf("result must name the project-relative path the next call takes:\n%s", got)
		}
		if strings.Contains(got, dir) {
			t.Errorf("a path under the root must not be printed as an absolute one:\n%s", got)
		}
	}
}

func TestAnOutsideResultKeepsTheAbsolutePath(t *testing.T) {
	root, dir := setup(t)
	write := NewWriteFile(root, dir)
	target := filepath.Join(filepath.Dir(dir), "outside-note.md")
	args, err := json.Marshal(map[string]string{"path": "../outside-note.md", "content": "written\n"})
	if err != nil {
		t.Fatal(err)
	}
	targets, err := write.Targets(args)
	if err != nil {
		t.Fatal(err)
	}
	got, err := write.Run(WithApprovals(context.Background(), []string{targets[0].Resolved}), args)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, target) {
		t.Errorf("an outside file must be named absolutely so it can be found again:\n%s", got)
	}
}

// read as written, a slashless root path would nest a copy of the root inside the project
func TestARootPathWithoutItsLeadingSlashIsTheRoot(t *testing.T) {
	root := "/private/var/folders/xy/T/run-1/project"
	for raw, want := range map[string]string{
		"private/var/folders/xy/T/run-1/project/index.html": root + "/index.html",
		"private/var/folders/xy/T/run-1/project":            root,
		"index.html":                                        root + "/index.html",
		"private/notes.txt":                                 root + "/private/notes.txt", // a real folder that only starts the same way
	} {
		got := resolveTarget(raw, root)
		if got.Resolved != want || got.Outside {
			t.Errorf("resolveTarget(%q) = %q (outside %v), want %q inside", raw, got.Resolved, got.Outside, want)
		}
	}
	if got := resolveTarget("etc/passwd", "/"); got.Resolved != "/etc/passwd" {
		t.Errorf("a root of / changed how a relative path reads: %q", got.Resolved)
	}
}

// end to end: the slip lands the file in the project, where the user asked for it
func TestAWriteToTheRootWithoutItsSlashLandsInTheProject(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{})
	out, err := NewWriteFile(root, dir).Run(context.Background(),
		[]byte(`{"path":"`+strings.TrimPrefix(dir, "/")+`/index.html","content":"<p>hi</p>"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
		t.Errorf("index.html is not at the project root (%v); the tool said: %s", err, out)
	}
	if !strings.Contains(out, "created index.html") {
		t.Errorf("the reply does not name the project-relative path: %s", out)
	}
}
