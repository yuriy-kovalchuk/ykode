// Package home resolves ykode's per-user directory so every caller agrees on where its files live.
package home

import (
	"fmt"
	"os"
	"path/filepath"
)

// dir caches the resolved root so the env is read once per run.
var dir string

// Dir is $YKODE_HOME when set, otherwise $HOME/.ykode.
func Dir() string {
	if dir != "" {
		return dir
	}
	if custom := os.Getenv("YKODE_HOME"); custom != "" {
		dir = custom
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	dir = filepath.Join(home, ".ykode")
	return dir
}

// Reset forgets the cached root; tests change $HOME and $YKODE_HOME.
func Reset() { dir = "" }

// Ensure creates a sub directory such as "logs" and returns its path.
func Ensure(sub string) (string, error) {
	full := filepath.Join(Dir(), sub)
	// what lives under here is command output, saved results and the audit log
	if err := os.MkdirAll(full, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", full, err)
	}
	if err := os.Chmod(full, 0o700); err != nil { // a directory made before this was readable by the machine
		return "", fmt.Errorf("protect %s: %w", full, err)
	}
	return full, nil
}

// Settings is the path of the per-user settings file; it may not exist.
func Settings() string { return filepath.Join(Dir(), "settings.yaml") }
