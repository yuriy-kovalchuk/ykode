package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// a malformed schema is invisible until the request fails to encode, so check them here
func TestBuiltInToolSchemasAreValid(t *testing.T) {
	root, dir := setup(t)
	registry := BuiltIn(root, dir, Options{})
	if len(registry.All()) != 7 {
		t.Fatalf("built-in tool count = %d, want 7", len(registry.All()))
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
	reads, listing := NewReadFile(root, dir), NewListDir(root, dir)

	registry := NewRegistry(reads, listing, NewReadFile(root, dir))

	if got := strings.Join(registry.Names(), ","); got != "read_file,list_dir" {
		t.Fatalf("names = %q, want registration order with the duplicate gone", got)
	}
	if len(registry.All()) != 2 {
		t.Fatalf("%d tools registered, want 2", len(registry.All()))
	}
	if _, ok := registry.Get("list_dir"); !ok {
		t.Fatal("Get must find a registered tool")
	}
	if _, ok := registry.Get("delete_everything"); ok {
		t.Fatal("Get must not invent a tool")
	}
}
