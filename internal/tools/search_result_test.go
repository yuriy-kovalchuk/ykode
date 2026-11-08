package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAFilteredSearchThatFindsNothingNamesWhereThePatternIs(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"server/main.go":   "package server\n\nfunc main() {}\n",
		"config/prod.yaml": "server:\n  port: 8443\n",
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	for _, tool := range []*SearchText{{scope: scope{root: root, dir: dir}}, NewSearchText(root, dir)} {
		out, err := tool.Run(context.Background(), json.RawMessage(`{"pattern":"port","glob":"*.go"}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "config/prod.yaml") || !strings.Contains(out, "*.go") {
			t.Errorf("rg=%v: a miss under the glob did not say where the pattern is:\n%s", tool.rgPath != "", out)
		}
		out, err = tool.Run(context.Background(), json.RawMessage(`{"pattern":"nowhere","glob":"*.go"}`))
		if err != nil || out != noMatches {
			t.Errorf("rg=%v: a pattern found nowhere = %q, %v", tool.rgPath != "", out, err)
		}
	}
}

func TestSearchShowsTheLineExactlyAsTheFileHasIt(t *testing.T) {
	root, dir := treeFixture(t, map[string]string{"a.go": "package x\n\nconst (\n\tMaxRetries = 3 // n\n)\n"})
	for _, tool := range []*SearchText{{scope: scope{root: root, dir: dir}}, NewSearchText(root, dir)} {
		out, err := tool.Run(context.Background(), json.RawMessage(`{"pattern":"MaxRetries"}`))
		if err != nil {
			t.Fatal(err)
		}
		// a line copied from here is what edit_file gets as old_text, so the tab must stay a tab
		if !strings.Contains(out, "4:\tMaxRetries = 3") {
			t.Errorf("rg=%v: the leading tab was not kept: %q", tool.rgPath != "", out)
		}
	}
}
