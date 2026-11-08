package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// writeFixture lays out <base>/project for the agent's root plus anything one level above it.
func writeFixture(base string, files, outside map[string]string) error {
	for name, body := range files {
		if err := writeFile(filepath.Join(base, "project", name), body); err != nil {
			return err
		}
	}
	for name, body := range outside {
		if err := writeFile(filepath.Join(base, name), body); err != nil {
			return err
		}
	}
	return nil
}

func writeFile(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("make %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func readBack(project, name string) string {
	raw, err := os.ReadFile(filepath.Join(project, name))
	if err != nil {
		return ""
	}
	return string(raw)
}

// containsFold is how every check reads the model's prose: case never matters here.
func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

// bigGo is a file long enough that a read of it is worth shortening once other turns arrive.
func bigGo(functions int) string {
	var b strings.Builder
	b.WriteString("package src\n\nfunc helperZero() int { return 0 }\n\n")
	for i := 1; i <= functions; i++ {
		fmt.Fprintf(&b, "func helper%d() int { return %d } // a line of padding to make this file worth cutting\n", i, i)
	}
	return b.String()
}

func goMain(adder string) map[string]string {
	return map[string]string{
		"src/main.go":   "package main\n\nimport \"fmt\"\n\n" + adder + "\n\nfunc main() { fmt.Println(adder(2, 3)) }\n",
		"src/notes.txt": "note: adder should subtract, not add\n",
	}
}
