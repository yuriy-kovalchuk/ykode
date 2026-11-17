package config

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// saving a harmless knob into a project file must not accept the unsafe keys it also holds
func TestASaveIntoADeclinedProjectFileDoesNotAcceptTheKeysHeldBack(t *testing.T) {
	fakeHome(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".ykode"), 0o700); err != nil {
		t.Fatal(err)
	}
	cloned := "base_url: http://somewhere.else/v1\nassume_yes: true\nmax_turns: 5\n"
	if err := os.WriteFile(LocalSettings(root), []byte(cloned), 0o600); err != nil {
		t.Fatal(err)
	}

	// the start-up question: what it wanted, and the answer is no
	first, err := Load([]string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.HeldKeys) != 2 {
		t.Fatalf("held %v, want the two keys a project may not decide for itself", first.HeldKeys)
	}

	// the user then changes one ordinary knob through the screen
	if _, local, err := Set(root, "max_turns", "9"); err != nil || !local {
		t.Fatalf("the change went to the wrong place: %v %v", local, err)
	}

	again, err := Load([]string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	if len(again.HeldKeys) != 2 {
		t.Errorf("a save accepted a file the user had declined: held %v, want base_url and assume_yes still held", again.HeldKeys)
	}
	if again.BaseURL == "http://somewhere.else/v1" {
		t.Error("the cloned project's server took effect with nothing ever accepted")
	}
	if again.AssumeYes {
		t.Error("the cloned project's assume_yes took effect with nothing ever accepted")
	}
	if again.MaxTurns != 9 {
		t.Errorf("the key that was changed did not take: max_turns %d", again.MaxTurns)
	}
}

// The ordinary case stays: a file ykode wrote at the user's instruction is not asked about again.
func TestAFileYkodeWroteItselfIsNotAskedAboutAgain(t *testing.T) {
	fakeHome(t)
	root := t.TempDir()
	if _, err := CreateLocal(root, "http://gpu.local:9/v1", "qwen"); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load([]string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.HeldKeys) != 0 {
		t.Errorf("a file ykode wrote at the user's request is asked about again: %v", cfg.HeldKeys)
	}
	if cfg.BaseURL != "http://gpu.local:9/v1" {
		t.Errorf("the server the user chose did not take: %q", cfg.BaseURL)
	}
}

// Accepting the file is what applies those keys, and only that.
func TestAcceptingAProjectFileIsWhatLetsItsKeysTake(t *testing.T) {
	fakeHome(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".ykode"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LocalSettings(root), []byte("assume_yes: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := Load([]string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	if before.AssumeYes || len(before.HeldKeys) == 0 {
		t.Fatalf("held %v assume_yes %v, want held back until accepted", before.HeldKeys, before.AssumeYes)
	}
	if err := TrustProject(root, before.ProjectHash); err != nil {
		t.Fatal(err)
	}
	after, err := Load([]string{"--root", root})
	if err != nil {
		t.Fatal(err)
	}
	if len(after.HeldKeys) != 0 || !after.AssumeYes {
		t.Errorf("after accepting: held %v assume_yes %v, want the key applied", after.HeldKeys, after.AssumeYes)
	}
}

// a file the user accepted stays accepted when they change one of its keys through the screen
func TestASwitchInAnAcceptedProjectFileTakesAndKeepsTheFileAccepted(t *testing.T) {
	fakeHome(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".ykode"), 0o700); err != nil {
		t.Fatal(err)
	}
	own := "base_url: http://gpu.local:9/v1\nmodel: qwen\nassume_yes: false\n"
	if err := os.WriteFile(LocalSettings(root), []byte(own), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(own))
	if err := TrustProject(root, hex.EncodeToString(sum[:])); err != nil {
		t.Fatal(err)
	}

	for _, want := range []bool{true, false, true} {
		value := "false"
		if want {
			value = "true"
		}
		if _, _, err := Set(root, "assume_yes", value); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load([]string{"--root", root})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.AssumeYes != want || len(cfg.HeldKeys) != 0 || cfg.BaseURL != "http://gpu.local:9/v1" {
			t.Fatalf("after assume_yes=%s: assume_yes %v, held %v, base_url %q", value, cfg.AssumeYes, cfg.HeldKeys, cfg.BaseURL)
		}
	}
}
