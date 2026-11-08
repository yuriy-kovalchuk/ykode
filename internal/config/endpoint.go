package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/yuriy-kovalchuk/ykode/internal/home"
)

// SaveEndpoint edits settings.yaml in place, so the user's comments and other keys survive.
func SaveEndpoint(baseURL, apiKey, model string) (string, error) {
	path := home.Settings()
	doc, err := readSettingsDoc()
	if err != nil {
		return path, err
	}
	top := doc.Content[0]
	blockStyle(top) // a file that started as JSON reads as YAML from now on
	setKey(top, "base_url", text(baseURL))
	if apiKey != "" {
		setKey(top, "api_key", text(apiKey))
	}
	if model != "" {
		setKey(top, "model", text(model))
	}
	return path, writeSettingsDoc(doc)
}

// readSettingsDoc opens settings.yaml as a document - or the built-in defaults when nothing is
// there yet - so an edit keeps the comments and the keys nobody asked about.
func readSettingsDoc() (*yaml.Node, error) {
	path := home.Settings()
	raw, err := os.ReadFile(settingsIn(home.Dir())) // the YAML file, or a legacy JSON one
	if errors.Is(err, os.ErrNotExist) {
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
func writeSettingsDoc(doc *yaml.Node) error {
	path := home.Settings()
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

// blockStyle undoes JSON's braces and quotes, so a carried-over file reads like the others.
func blockStyle(n *yaml.Node) {
	n.Style = 0
	for _, c := range n.Content {
		blockStyle(c)
	}
}
