package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func undoFixture(t *testing.T) (*Registry, string, *UndoStack) {
	t.Helper()
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	stack := &UndoStack{}
	return BuiltIn(root, dir, Options{Undo: stack, MaxResultBytes: 4000}), dir, stack
}

func mustTool(t *testing.T, registry *Registry, name string) Tool {
	t.Helper()
	tool, ok := registry.Get(name)
	if !ok {
		t.Fatalf("no %s tool", name)
	}
	return tool
}

func TestAWriteCanBePutBack(t *testing.T) {
	registry, dir, stack := undoFixture(t)
	write := mustTool(t, registry, "write_file")
	for _, content := range []string{"one\n", "two\n"} {
		args, err := json.Marshal(map[string]string{"path": "a.txt", "content": content})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := write.Run(context.Background(), args); err != nil {
			t.Fatal(err)
		}
	}

	said, err := stack.Undo(false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "one\n" {
		t.Errorf("a.txt is %q after undo, want the earlier content", got)
	}
	if said == "" {
		t.Error("the undo should say what it did")
	}

	// the second write is still on the stack: undoing again removes the created file
	if _, err := stack.Undo(false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); !os.IsNotExist(err) {
		t.Errorf("the created file survived its second undo: %v", err)
	}
}

func TestAnEditIsUndoneToTheExactEarlierBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	original := "package main\n\nfunc main() {\n\tprintln(1)\n}\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	stack := &UndoStack{}
	registry := BuiltIn(root, dir, Options{Undo: stack})
	edit := mustTool(t, registry, "edit_file")
	args := json.RawMessage(`{"path":"main.go","edits":[{"old_string":"println(1)","new_string":"println(2)"}]}`)
	if _, err := edit.Run(context.Background(), args); err != nil {
		t.Fatal(err)
	}
	if _, err := stack.Undo(false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("undo changed the file:\ngot %q\nwant %q", got, original)
	}
}

func TestUndoWithNothingOnTheStackSaysSo(t *testing.T) {
	stack := &UndoStack{}
	said, err := stack.Undo(false)
	if err != nil {
		t.Fatal(err)
	}
	if said != "" {
		t.Errorf("an empty stack said %q", said)
	}
	if !stack.Empty() {
		t.Error("an empty stack is not empty")
	}
}

func TestAnOutsideWriteIsUndoneWhereItLives(t *testing.T) {
	project := t.TempDir()
	elsewhere := t.TempDir()
	path := filepath.Join(elsewhere, "notes.md")
	if err := os.WriteFile(path, []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(project)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	stack := &UndoStack{}
	registry := BuiltIn(root, project, Options{Undo: stack})
	write := mustTool(t, registry, "write_file")

	ctx := WithApprovals(context.Background(), []string{path})
	args := json.RawMessage(`{"path":"` + filepath.ToSlash(path) + `","content":"after\n"}`)
	if _, err := write.Run(ctx, args); err != nil {
		t.Fatal(err)
	}
	if _, err := stack.Undo(false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "before\n" {
		t.Errorf("the outside file is %q, want what it was", got)
	}
}

func TestUndoRefusesToOverwriteAChangeMadeByHandSince(t *testing.T) {
	registry, dir, stack := undoFixture(t)
	write := mustTool(t, registry, "write_file")
	for _, content := range []string{"one\n", "two\n"} {
		args, _ := json.Marshal(map[string]string{"path": "a.txt", "content": content})
		if _, err := write.Run(context.Background(), args); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("my own fix\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := stack.Undo(false); err == nil {
		t.Fatal("undo wrote over a change made after the tool's")
	}
	if got, _ := os.ReadFile(path); string(got) != "my own fix\n" {
		t.Errorf("a refused undo still changed the file: %q", got)
	}
	if _, err := stack.Undo(true); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "one\n" {
		t.Errorf("a forced undo did not put the earlier text back: %q", got)
	}
}
