package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

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

// SettingsTarget is the file a change goes to: the project's when it has one, else the user's own
func SettingsTarget(root string) (path string, local bool) {
	if HasLocal(root) {
		return LocalSettings(root), true
	}
	return home.Settings(), false
}

// trustWritten records the project file as accepted, since ykode wrote it, without accepting unsafe keys
func trustWritten(root string, wasAccepted bool, wrote ...string) error {
	raw, err := os.ReadFile(LocalSettings(root))
	if err != nil {
		return err
	}
	if !wasAccepted {
		held, _ := unsafeIn(raw)
		for _, name := range held {
			if !contains(wrote, name) {
				return nil // leave the acceptance as it was: the next start asks about those keys again
			}
		}
	}
	sum := sha256.Sum256(raw)
	return TrustProject(root, hex.EncodeToString(sum[:]))
}

// localAccepted says the project's settings file, as it stands now, is the one the user accepted.
func localAccepted(root string) bool {
	raw, err := os.ReadFile(LocalSettings(root))
	return err == nil && acceptedProject(root, raw)
}

// unsafeIn lists the unsafe keys a settings file asks for, the same question the loader asks
func unsafeIn(raw []byte) (names, shown []string) {
	var s fileSettings
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if dec.Decode(&s) != nil {
		return nil, nil // an unreadable file is the loader's to report, not this one's
	}
	return stripUnsafe(&s)
}

// acceptedProject says the project's file is still the accepted one: any edit changes the hash
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
