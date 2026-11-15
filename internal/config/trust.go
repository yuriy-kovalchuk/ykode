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

// LocalSettings is where a project keeps its own ykode settings: <root>/.ykode/settings.yaml.
func LocalSettings(root string) string { return settingsIn(filepath.Join(root, ".ykode")) }

// HasLocal says the project has a settings file of its own.
func HasLocal(root string) bool {
	_, err := os.Stat(LocalSettings(root))
	return err == nil
}

// SettingsTarget is the file a change is written to: the project's when it has one, else the user's
// own. The bool says it is the project's.
func SettingsTarget(root string) (path string, local bool) {
	if HasLocal(root) {
		return LocalSettings(root), true
	}
	return home.Settings(), false
}

// trustWritten records the project file as it now stands as accepted: ykode wrote it, so the next
// start has nothing to ask about.
func trustWritten(root string) error {
	raw, err := os.ReadFile(LocalSettings(root))
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	return TrustProject(root, hex.EncodeToString(sum[:]))
}

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
