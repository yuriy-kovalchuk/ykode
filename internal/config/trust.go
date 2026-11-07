package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yuriy-kovalchuk/ykode/internal/home"
)

// trustRecord is where the acceptances live, under the per-user directory.
const trustRecord = "trusted.json"

// projectSettingsPath is where a project keeps its own ykode settings.
func projectSettingsPath(root string) string { return filepath.Join(root, ".ykode", "settings.json") }

// acceptedProject says whether the project's settings file is still the one that was accepted:
// any edit changes the hash, and the run asks about it again.
func acceptedProject(root string, raw []byte) bool {
	records, err := readTrust()
	if err != nil {
		return false
	}
	sum := sha256.Sum256(raw)
	return records[root] == hex.EncodeToString(sum[:])
}

// TrustProject records a project's settings file as accepted, so the next run applies it whole.
func TrustProject(root string) error {
	settings := projectSettingsPath(root)
	raw, err := os.ReadFile(settings)
	if err != nil {
		return fmt.Errorf("trust %s: %w", settings, err)
	}
	records, err := readTrust()
	if err != nil {
		records = map[string]string{}
	}
	sum := sha256.Sum256(raw)
	records[root] = hex.EncodeToString(sum[:])
	out, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(home.Dir(), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", home.Dir(), err)
	}
	path := filepath.Join(home.Dir(), trustRecord)
	if err := os.WriteFile(path, append(out, '\n'), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// readTrust reads the acceptances; nothing recorded is not an error worth carrying.
func readTrust() (map[string]string, error) {
	raw, err := os.ReadFile(filepath.Join(home.Dir(), trustRecord))
	if err != nil {
		return nil, err
	}
	records := map[string]string{}
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil, fmt.Errorf("%s: %w", trustRecord, err)
	}
	return records, nil
}
