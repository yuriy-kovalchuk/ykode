package home

import (
	"os"
	"path/filepath"
	"testing"
)

func TestYkodeHomeWins(t *testing.T) {
	t.Setenv("YKODE_HOME", "/tmp/custom-ykode")
	Reset()
	defer Reset()

	if Dir() != "/tmp/custom-ykode" {
		t.Fatalf("Dir() = %q, want the env override", Dir())
	}
}

func TestTheDefaultIsADotYkodeInTheUserHome(t *testing.T) {
	t.Setenv("YKODE_HOME", "")
	t.Setenv("HOME", "/tmp/fake-home")
	Reset()
	defer Reset()

	if Dir() != filepath.Join("/tmp/fake-home", ".ykode") {
		t.Fatalf("Dir() = %q", Dir())
	}
	if Settings() != filepath.Join("/tmp/fake-home", ".ykode", "settings.yaml") {
		t.Fatalf("Settings() = %q", Settings())
	}
}

func TestEnsureBuildsTheSubDirectory(t *testing.T) {
	t.Setenv("YKODE_HOME", t.TempDir())
	Reset()
	defer Reset()

	if err := os.MkdirAll(filepath.Join(Dir(), "logs"), 0o755); err != nil { // as an older ykode made it
		t.Fatal(err)
	}
	logs, err := Ensure("logs")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(logs)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", logs)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("%s is %o: under ~/.ykode sit command output and saved results", logs, info.Mode().Perm())
	}
}
