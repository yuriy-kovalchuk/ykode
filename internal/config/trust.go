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

// SettingsTarget is the file a change is written to: the project's when it has one, else the user's
// own. The bool says it is the project's.
func SettingsTarget(root string) (path string, local bool) {
	if HasLocal(root) {
		return LocalSettings(root), true
	}
	return home.Settings(), false
}

// trustWritten records the project file as it now stands as accepted: ykode wrote it, so the next start
// has nothing to ask about. A file accepted before this write stays accepted: without that, switching
// assume_yes in a file that also names a server held every safety key back, assume_yes included. A file
// that was not accepted is accepted only when every unsafe key it asks for is one this call wrote - a
// file the user declined and then saved a harmless knob into must not retire the warning unseen.
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

// unsafeIn lists the unsafe keys a settings file asks for, without reading the rest of the run's
// settings: the question "would this file need accepting?" is the same one the loader asks.
func unsafeIn(raw []byte) (names, shown []string) {
	var s fileSettings
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if dec.Decode(&s) != nil {
		return nil, nil // an unreadable file is the loader's to report, not this one's
	}
	return stripUnsafe(&s)
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
