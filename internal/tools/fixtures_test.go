package tools

import (
	"os"
	"path/filepath"
	"testing"
)

// treeFixture writes files into a fresh temp dir, creating parent directories
// as needed, and returns the open Root a tool needs plus the directory itself.
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
