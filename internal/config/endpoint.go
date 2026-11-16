package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/yuriy-kovalchuk/ykode/internal/home"
)

// SaveEndpoint edits the settings file this project uses in place, so the user's comments and other
// keys survive. It answers the file it wrote and whether that was the project's own.
func SaveEndpoint(root, baseURL, apiKey, model string) (string, bool, error) {
	path, local := SettingsTarget(root)
	wasAccepted := local && localAccepted(root)
	doc, err := readSettingsDoc(path)
	if err != nil {
		return path, local, err
	}
	top := doc.Content[0]
	setKey(top, "base_url", text(baseURL))
	if apiKey != "" {
		setKey(top, "api_key", text(apiKey))
	}
	if model != "" {
		setKey(top, "model", text(model))
	}
	if err := writeSettingsDoc(path, doc); err != nil {
		return path, local, err
	}
	if local {
		if err := trustWritten(root, wasAccepted, "base_url", "api_key", "model"); err != nil {
			return path, local, err
		}
	}
	return path, local, nil
}

// readSettingsDoc opens one settings file as a document - or the built-in defaults when the user's own
// file is not there yet - so an edit keeps the comments and the keys nobody asked about. A project
// file that is missing is an error: there is nothing to edit.
func readSettingsDoc(path string) (*yaml.Node, error) {
	raw, err := os.ReadFile(settingsIn(filepath.Dir(path)))
	if errors.Is(err, os.ErrNotExist) {
		if filepath.Dir(path) != home.Dir() {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		raw, err = yaml.Marshal(builtinSettings())
	}
	if err != nil {
		return nil, fmt.Errorf("read settings: %w", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if doc.Kind == 0 { // an empty file
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: the top level is not a mapping of keys", path)
	}
	return &doc, nil
}

// writeSettingsDoc saves a document back, readable only by you, because it may hold a token.
func writeSettingsDoc(path string, doc *yaml.Node) error {
	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return os.Chmod(path, 0o600)
}
