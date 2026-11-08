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
	raw, err := os.ReadFile(settingsIn(home.Dir())) // the YAML file, or a legacy JSON one
	if errors.Is(err, os.ErrNotExist) {
		raw, err = yaml.Marshal(builtinSettings())
	}
	if err != nil {
		return path, fmt.Errorf("read settings: %w", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return path, fmt.Errorf("%s: %w", path, err)
	}
	if doc.Kind == 0 { // an empty file
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	top := doc.Content[0]
	if top.Kind != yaml.MappingNode {
		return path, fmt.Errorf("%s: the top level is not a mapping of keys", path)
	}
	blockStyle(top) // a file that started as JSON reads as YAML from now on
	setKey(top, "base_url", baseURL)
	if apiKey != "" {
		setKey(top, "api_key", apiKey)
	}
	if model != "" {
		setKey(top, "model", model)
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return path, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return path, fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	// 0600: the file may hold a key from now on
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return path, fmt.Errorf("write %s: %w", path, err)
	}
	return path, os.Chmod(path, 0o600)
}

// setKey sets one top-level key, replacing its value where it already has one.
func setKey(m *yaml.Node, key, value string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

// blockStyle undoes JSON's braces and quotes, so a carried-over file reads like the others.
func blockStyle(n *yaml.Node) {
	n.Style = 0
	for _, c := range n.Content {
		blockStyle(c)
	}
}
