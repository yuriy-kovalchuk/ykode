package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Set writes one knob into the settings file this project uses, keeping the comments and every other
// key, and names the file it wrote and whether that was the project's own. The value is checked
// before anything is touched: a knob only takes what it can use.
func Set(root, key, value string) (string, bool, error) {
	k, ok := knobFor(key)
	if !ok {
		return "", false, fmt.Errorf("%s is not a setting: settings lists what there is", key)
	}
	path, local := SettingsTarget(root)
	doc, err := readSettingsDoc(path)
	if err != nil {
		return path, local, err
	}
	top := doc.Content[0]
	if k.kind == kindList {
		if err := addToList(top, path, k.key, value); err != nil {
			return path, local, err
		}
	} else {
		scalar, err := scalarFor(k, value)
		if err != nil {
			return path, local, err
		}
		setPath(top, k.key, scalar)
	}
	if err := writeSettingsDoc(path, doc); err != nil {
		return path, local, err
	}
	if local {
		if err := trustWritten(root); err != nil {
			return path, local, err
		}
	}
	return path, local, nil
}

// scalarFor checks a value against the kind of its knob and returns the node to write.
func scalarFor(k knob, value string) (*yaml.Node, error) {
	value = strings.TrimSpace(value)
	if len(k.options) > 0 && !contains(k.options, value) {
		return nil, fmt.Errorf("%s is not one of: %s", value, strings.Join(k.options, ", "))
	}
	switch k.kind {
	case kindCount:
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("%s wants a whole number of tokens (0 leaves the answer the room the window has)", k.key)
		}
		return num(n), nil
	case kindSeconds, kindDays:
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("%s wants a whole number above zero", k.key)
		}
		return num(n), nil
	case kindYesNo:
		on, ok := switchState(value)
		if !ok {
			return nil, fmt.Errorf("%s wants on or off", k.key)
		}
		return boolNode(on), nil
	case kindHours:
		d, err := time.ParseDuration(value)
		if err != nil || d < 0 {
			return nil, fmt.Errorf("%s wants a length of time, like 720h", k.key)
		}
		return text(hours(d)), nil
	default:
		if value == "" {
			return nil, fmt.Errorf("%s cannot be empty; a setting left out goes back to its default by deleting its line", k.key)
		}
		return text(value), nil
	}
}

// addToList puts one more item on a list setting, which is how the never-ask list grows.
func addToList(top *yaml.Node, path, key, value string) error {
	if value == "" {
		return fmt.Errorf("what goes on %s cannot be empty", key)
	}
	list := listNode(top, key)
	if list.Kind != yaml.SequenceNode {
		return fmt.Errorf("%s is not a list in %s", key, path)
	}
	for _, item := range list.Content {
		if item.Value == value {
			return fmt.Errorf("%s is already on %s", value, key)
		}
	}
	list.Content = append(list.Content, text(value))
	return nil
}

// setPath sets one key, made of sections when it sits inside one: context.safety_margin.
func setPath(top *yaml.Node, key string, value *yaml.Node) {
	sections := strings.Split(key, ".")
	node := top
	for _, section := range sections[:len(sections)-1] {
		node = subMap(node, section)
	}
	setKey(node, sections[len(sections)-1], value)
}

// listNode finds a list setting, writing the sections and the empty list when the file has not got
// them yet.
func listNode(top *yaml.Node, key string) *yaml.Node {
	sections := strings.Split(key, ".")
	node := top
	for _, section := range sections[:len(sections)-1] {
		node = subMap(node, section)
	}
	last := sections[len(sections)-1]
	list := childOf(node, last)
	if list == nil {
		list = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: last}, list)
	}
	return list
}

// subMap finds a section, writing an empty one when the file has not got it yet.
func subMap(top *yaml.Node, name string) *yaml.Node {
	if got := childOf(top, name); got != nil {
		return got
	}
	section := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	top.Content = append(top.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, section)
	return section
}

func childOf(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func setKey(m *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = value
			return
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}

func text(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func num(n int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(n)}
}

func boolNode(on bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(on)}
}

// switchState reads a switch the way the screen writes one.
func switchState(value string) (bool, bool) {
	switch strings.ToLower(value) {
	case "on", "yes", "true", "1":
		return true, true
	case "off", "no", "false", "0":
		return false, true
	}
	return false, false
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
