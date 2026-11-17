package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// localSeed is what a new project file starts with: the server and model this run is using.
type localSeed struct {
	BaseURL string `yaml:"base_url"`
	Model   string `yaml:"model,omitempty"`
}

// CreateLocal writes <root>/.ykode/settings.yaml for this run, trusts it, and never overwrites one
func CreateLocal(root, baseURL, model string) (string, error) {
	path := filepath.Join(root, ".ykode", "settings.yaml")
	if HasLocal(root) {
		return path, fmt.Errorf("%s is already there: /settings changes it", LocalSettings(root))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return path, fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	body, err := yaml.Marshal(localSeed{BaseURL: baseURL, Model: model})
	if err != nil {
		return path, err
	}
	header := "# ykode settings for this project. A key here overrides ~/.ykode/settings.yaml;\n" +
		"# a key left out keeps the global value. /settings edits this file.\n"
	if err := os.WriteFile(path, append([]byte(header), body...), 0o600); err != nil {
		return path, fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return path, err
	}
	// the file is new and holds only what this call wrote, so those are the keys it records
	return path, trustWritten(root, false, "base_url", "model")
}
