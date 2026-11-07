package tools

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// a malformed schema is invisible until the request fails to encode, so check them here
func TestBuiltInToolSchemasAreValid(t *testing.T) {
	root, dir := setup(t)
	registry := BuiltIn(root, dir, Options{})
	if len(registry.All()) != 6 {
		t.Fatalf("built-in tool count = %d, want 6", len(registry.All()))
	}
	for _, tool := range registry.All() {
		if tool.Name() == "" || tool.Description() == "" {
			t.Errorf("%T: name and description must be set", tool)
		}
		var schema map[string]any
		if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
			t.Errorf("%s: schema is not valid JSON: %v", tool.Name(), err)
			continue
		}
		if schema["type"] != "object" {
			t.Errorf("%s: schema type = %v, want object", tool.Name(), schema["type"])
		}
	}
}

func TestRegistryKeepsRegistrationOrderAndDropsDuplicates(t *testing.T) {
	root, dir := setup(t)
	reads, listing := NewReadFile(root, dir), NewFindFiles(root, dir)

	registry := NewRegistry(reads, listing, NewReadFile(root, dir))

	if got := strings.Join(registry.Names(), ","); got != "read_file,find_files" {
		t.Fatalf("names = %q, want registration order with the duplicate gone", got)
	}
	if len(registry.All()) != 2 {
		t.Fatalf("%d tools registered, want 2", len(registry.All()))
	}
	if _, ok := registry.Get("find_files"); !ok {
		t.Fatal("Get must find a registered tool")
	}
	if _, ok := registry.Get("delete_everything"); ok {
		t.Fatal("Get must not invent a tool")
	}
}

// Detailed is only safe for tools that never run beside each other, and the agent decides that
// by name, so pin the half of the built-in set that may overlap.
func TestNoOverlappingToolRemembersItsLastCall(t *testing.T) {
	root, dir := setup(t)
	store := NewResultStore(filepath.Join(dir, "results"))
	overlapping := map[string]bool{"find_files": true, "search_text": true, "read_file": true, "recall": true}

	for _, tool := range BuiltIn(root, dir, Options{Store: store}).All() {
		if !overlapping[tool.Name()] {
			continue
		}
		if _, ok := tool.(Detailed); ok {
			t.Errorf("%s can run in parallel and remembers its last call: the audit would get another call's detail", tool.Name())
		}
	}
}
