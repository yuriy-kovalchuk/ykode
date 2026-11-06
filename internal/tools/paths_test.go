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
		got, err := resolveTarget(c.raw, root)
		if err != nil {
			t.Errorf("%q: %v", c.raw, err)
			continue
		}
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
