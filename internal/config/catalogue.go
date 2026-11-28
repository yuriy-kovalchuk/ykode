package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/yuriy-kovalchuk/ykode/internal/home"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/theme"
	"github.com/yuriy-kovalchuk/ykode/internal/tools"
)

// Where a value came from, said the way a screen reports it.
const (
	FromDefault = "default"
	FromFile    = "file"
	FromProject = "project file"
	FromEnv     = "env"
	FromFlag    = "flag"
)

// kinds of value a knob takes, which is also what Set checks before it writes anything.
const (
	kindText = iota
	kindCount
	kindYesNo
	kindHours
	kindSeconds
	kindList // written as a list; a new value is added to what is there
)

// knob is one thing settings.yaml can change: its key, what it is for, and the kind of value it takes
type knob struct {
	key     string
	label   string // what a screen calls it
	group   string // the heading a screen lists it under
	unit    string // what its number counts, for a screen to say after it; "" for none
	note    string
	kind    int
	options []string // the choices when there are few, or what a list's entries may be; anything fits when nil
	value   func(Config) string
	items   func(Config) []string // a list's entries one by one; nil for a knob that is not a list
	live    bool                  // a running session takes this up at once; the rest wait for its next start
}

// the headings a screen groups the knobs under, in the order it shows them
const (
	GroupModel       = "Model"
	GroupContext     = "Context"
	GroupTools       = "Tools"
	GroupPermissions = "Permissions"
	GroupSessions    = "Sessions"
	GroupAppearance  = "Appearance"
)

// knobs are every setting the file can set, the one place a key is described: screens and checks read it
func knobs() []knob {
	return []knob{
		{key: "base_url", label: "Server", group: GroupModel, kind: kindText,
			note: "the server ykode asks; a first session asks for it", value: func(c Config) string { return c.BaseURL }},
		{key: "model", label: "Model", group: GroupModel, kind: kindText,
			note: "model id; empty takes the first one the server lists", value: func(c Config) string { return c.Model }},
		{key: "thinking_level", label: "Thinking level", group: GroupModel, kind: kindText, live: true,
			note:    "how deeply the model thinks before answering, sent as reasoning_effort; a word the model does not take goes as the nearest it does",
			options: []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"},
			value:   func(c Config) string { return c.ThinkingLevel }},
		{key: "thinking_budget", label: "Thinking cap", group: GroupModel, unit: "tokens", kind: kindCount, live: true,
			note:  fmt.Sprintf("tokens the thinking phase may take; empty leaves it what the reply can spare, %d kept for the answer", llm.MinAnswerTokens),
			value: func(c Config) string { return count(c.ThinkingBudget) }},
		{key: "max_tokens", label: "Reply cap", group: GroupModel, unit: "tokens", kind: kindCount, live: true,
			note:  "tokens per answer; empty leaves the answer the room the window has left",
			value: func(c Config) string { return count(c.MaxTokens) }},
		{key: "max_turns", label: "Round-trips per ask", group: GroupModel, kind: kindCount, live: true,
			note:  "round-trips one ask may take before it is stopped as runaway; 0 for no limit",
			value: func(c Config) string { return count(c.MaxTurns) }},
		{key: "context.safety_margin", label: "Room kept for the answer", group: GroupContext, unit: "tokens",
			kind: kindCount, live: true, note: "tokens kept free for the answer when no cap was asked for",
			value: func(c Config) string { return fmt.Sprint(c.SafetyMargin) }},
		{key: "stream_idle_sec", label: "Silence before a stream is dead", group: GroupContext, unit: "s",
			kind: kindSeconds, note: "seconds of silence before a stream counts as dead; a cold prompt needs more",
			value: func(c Config) string { return fmt.Sprint(int(c.StreamIdle / time.Second)) }},
		{key: "save_server_cache", label: "Keep the server's cache", group: GroupContext, kind: kindYesNo,
			note: "llama.cpp saves a conversation's cache on leaving and loads it on --continue; " +
				"needs --slot-save-path, and a long one takes gigabytes",
			value: func(c Config) string { return yesNo(c.SaveServerCache) }},
		{key: "tools.disabled", label: "Tools switched off", group: GroupTools, kind: kindList, options: tools.Names(),
			note:  "tools the model is not offered, decided when a session starts",
			value: func(c Config) string { return strings.Join(c.DisabledTools, ", ") },
			items: func(c Config) []string { return c.DisabledTools }},
		{key: "tools.read_lines", label: "Lines per read", group: GroupTools, unit: "lines", kind: kindCount,
			note:  "lines read_file returns when the model does not ask for a window",
			value: func(c Config) string { return fmt.Sprint(c.ReadLines) }},
		{key: "tools.exec_timeout_sec", label: "Command time limit", group: GroupTools, unit: "s", kind: kindSeconds,
			note:  "seconds a command may run before it is cut",
			value: func(c Config) string { return fmt.Sprint(int(c.ExecTimeout / time.Second)) }},
		{key: "tools.keep_command_logs", label: "Keep command logs", group: GroupTools, kind: kindHours,
			note:  "how long a cut command's full output is kept",
			value: func(c Config) string { return hours(c.KeepCommandLogs) }},
		{key: "tools.command_log_dir", label: "Command log folder", group: GroupTools, kind: kindText,
			note:  "where that output goes; empty is under the home folder",
			value: func(c Config) string { return c.CommandLogDir }},
		{key: "assume_yes", label: "Skip write confirmations", group: GroupPermissions, kind: kindYesNo, live: true,
			note:  "do not ask about writes inside the project (outside it is always asked)",
			value: func(c Config) string { return yesNo(c.AssumeYes) }},
		{key: "permissions.allow", label: "Never ask about", group: GroupPermissions, kind: kindList,
			note:  "a tool name, or exec: and the start of a command, like exec:go test",
			value: func(c Config) string { return strings.Join(c.Allow, ", ") },
			items: func(c Config) []string { return c.Allow }},
		{key: "keep_sessions", label: "Keep conversations", group: GroupSessions, kind: kindHours,
			note:  "how long a saved conversation survives",
			value: func(c Config) string { return hours(c.KeepSessions) }},
		{key: "theme", label: "Theme", group: GroupAppearance, kind: kindText, live: true, options: theme.Names(),
			note:  "the colours the screen draws with; auto follows the terminal's background",
			value: func(c Config) string { return c.Theme }},
	}
}

// LiveKnobs are the settings a running session takes up at once, named from the list above
func LiveKnobs() map[string]bool {
	live := map[string]bool{}
	for _, k := range knobs() {
		if k.live {
			live[k.key] = true
		}
	}
	return live
}

// Kinds of value a screen edits, each with the menu that suits it.
const (
	KindText     = "text"
	KindCount    = "count"
	KindSeconds  = "seconds"
	KindDuration = "duration"
	KindSwitch   = "switch"
	KindList     = "list"
)

// kindNames are the kinds as a screen reads them.
var kindNames = map[int]string{kindText: KindText, kindCount: KindCount, kindSeconds: KindSeconds,
	kindHours: KindDuration, kindYesNo: KindSwitch, kindList: KindList}

// Setting is one knob as a screen shows it: key, purpose, current value, default, and who set it
type Setting struct {
	Key     string
	Label   string
	Group   string
	Unit    string
	Kind    string
	Note    string
	Value   string
	Items   []string // a list's entries, which Value joins with commas
	Default string
	From    string
	Options []string // the choices, or what a list's entries may be
	Live    bool     // a running session takes it up at once
	Switch  bool     // on or off: the screen toggles it
	List    bool     // a list: entries are added and taken off one at a time
}

// Catalogue lists the settings this run resolved, each with where its value came from.
func (c Config) Catalogue() []Setting {
	blank := defaults()
	global, project := fileKeys(c)
	list := make([]Setting, 0, len(knobs()))
	for _, k := range knobs() {
		value, def := k.value(c), k.value(blank)
		from := c.where(k.key, global, project)
		if from == FromFile && value == def {
			from = FromDefault // a file that only repeats the built-in has not chosen anything
		}
		var items []string
		if k.items != nil {
			items = append(items, k.items(c)...)
		}
		list = append(list, Setting{Key: k.key, Label: k.label, Group: k.group, Unit: k.unit, Kind: kindNames[k.kind],
			Note: k.note, Value: value, Items: items, Default: def, From: from, Options: k.options, Live: k.live,
			Switch: k.kind == kindYesNo, List: k.kind == kindList})
	}
	return list
}

// where says who set a knob: flag, then environment, then settings file, then the built-in
func (c Config) where(key string, global, project map[string]bool) string {
	if flag, ok := flagFor[key]; ok && c.flagsGiven[flag] {
		return FromFlag
	}
	if name, ok := envFor[key]; ok && os.Getenv(name) != "" {
		return FromEnv
	}
	if project[key] {
		return FromProject
	}
	if global[key] {
		return FromFile
	}
	return FromDefault
}

// the flags that set a knob, and the variables that do.
var flagFor = map[string]string{
	"base_url":        "base-url",
	"model":           "model",
	"max_turns":       "max-turns",
	"max_tokens":      "max-tokens",
	"thinking_level":  "thinking",
	"thinking_budget": "thinking-budget",
}
var envFor = map[string]string{
	"base_url": "LOCAL_LLM_URL",
	"model":    "LOCAL_LLM_MODEL",
}

// fileKeys are the knobs each settings file sets, read again here because the screen asks now
func fileKeys(c Config) (global, project map[string]bool) {
	global, project = map[string]bool{}, map[string]bool{}
	for _, each := range []struct {
		path  string
		found map[string]bool
	}{{settingsIn(home.Dir()), global}, {LocalSettings(c.Root), project}} {
		raw, err := os.ReadFile(each.path)
		if err != nil {
			continue
		}
		var doc yaml.Node
		if yaml.Unmarshal(raw, &doc) != nil || len(doc.Content) == 0 {
			continue
		}
		walkKeys(doc.Content[0], "", each.found)
	}
	return global, project
}

func walkKeys(node *yaml.Node, prefix string, into map[string]bool) {
	if node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		name := prefix + node.Content[i].Value
		switch node.Content[i+1].Kind {
		case yaml.MappingNode:
			walkKeys(node.Content[i+1], name+".", into)
		case yaml.ScalarNode:
			// a key written with nothing in it leaves the choice to the program, it does not make one
			if node.Content[i+1].Value == "" {
				continue
			}
		}
		into[name] = true
	}
}

func knobFor(key string) (knob, bool) {
	for _, k := range knobs() {
		if k.key == key {
			return k, true
		}
	}
	return knob{}, false
}

func yesNo(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// count says "no cap" for zero, because that is what zero means for these knobs.
func count(n int) string {
	if n == 0 {
		return "no cap"
	}
	return fmt.Sprint(n)
}

// ModelFromFile says the model came from a settings file, a remembered choice rather than a request
func (c Config) ModelFromFile() bool {
	global, project := fileKeys(c)
	switch c.where("model", global, project) {
	case FromFile, FromProject:
		return true
	}
	return false
}
