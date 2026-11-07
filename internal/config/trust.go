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
func projectSettingsPath(root string) string { return settingsIn(filepath.Join(root, ".ykode")) }

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

// TrustProject trusts the file the user was shown (its hash), so an edit made since is asked about again.
func TrustProject(root, hash string) error {
	records, err := readTrust()
	if err != nil {
		records = map[string]string{}
	}
	records[root] = hash
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
