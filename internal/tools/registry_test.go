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
	if len(registry.All()) != 5 {
		t.Fatalf("built-in tool count = %d, want 5", len(registry.All()))
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
	reads, running := NewReadFile(root, dir), NewExec(dir)

	registry := NewRegistry(reads, running, NewReadFile(root, dir))

	if got := strings.Join(registry.Names(), ","); got != "read_file,exec_command" {
		t.Fatalf("names = %q, want registration order with the duplicate gone", got)
	}
	if len(registry.All()) != 2 {
		t.Fatalf("%d tools registered, want 2", len(registry.All()))
	}
	if _, ok := registry.Get("exec_command"); !ok {
		t.Fatal("Get must find a registered tool")
	}
	if _, ok := registry.Get("delete_everything"); ok {
		t.Fatal("Get must not invent a tool")
	}
}

// Detailed is only safe for tools that never run beside each other, so pin the overlapping half
func TestNoOverlappingToolRemembersItsLastCall(t *testing.T) {
	root, dir := setup(t)
	overlapping := map[string]bool{"read_file": true}

	for _, tool := range BuiltIn(root, dir, Options{}).All() {
		if !overlapping[tool.Name()] {
			continue
		}
		if _, ok := tool.(Detailed); ok {
			t.Errorf("%s can run in parallel and remembers its last call: another call's change would be reported for it", tool.Name())
		}
	}
}

// a tool taken out leaves the rest in the order the model already saw them in
func TestARegistryWithoutAToolKeepsTheOthersInOrder(t *testing.T) {
	root, dir := setup(t)
	left := BuiltIn(root, dir, Options{}).Without("exec_command", "no_such_tool")
	if got := strings.Join(left.Names(), ","); got != "read_file,write_file,edit_file,plan" {
		t.Errorf("left %s", got)
	}
	if _, ok := left.Get("exec_command"); ok {
		t.Error("a tool taken out can still be called")
	}
}

// the names a setting may switch off are every tool a run can offer, ask_user included
func TestTheToolNamesAreEveryToolOnOffer(t *testing.T) {
	root, dir := setup(t)
	offered := append(BuiltIn(root, dir, Options{}).Names(), NewAskUser(nil).Name())
	if got, want := strings.Join(Names(), ","), strings.Join(offered, ","); got != want {
		t.Errorf("Names() = %s, but the tools are %s", got, want)
	}
}
