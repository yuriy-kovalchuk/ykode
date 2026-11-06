package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// treeFixture writes files into a fresh temp dir and returns the open Root plus the directory.
func treeFixture(t *testing.T, files map[string]string) (*os.Root, string) {
	t.Helper()
	dir := t.TempDir()
	for path, content := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root, dir
}

// runTool runs a tool with JSON arguments and fails the test if it errors.
func runTool(t *testing.T, tool Tool, args string) string {
	t.Helper()
	got, err := tool.Run(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("%s: %v", tool.Name(), err)
	}
	return got
}
