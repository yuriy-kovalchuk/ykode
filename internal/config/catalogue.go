package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/yuriy-kovalchuk/ykode/internal/home"
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
	note    string
	kind    int
	options []string // the choices when there are few; anything of its kind fits when nil
	value   func(Config) string
	live    bool // a running session takes this up at once; the rest wait for its next start
}

// knobs are every setting the file can set, the one place a key is described: screens and checks read it
func knobs() []knob {
	return []knob{
		{"base_url", "the server ykode asks; a first session asks for it", kindText, nil,
			func(c Config) string { return c.BaseURL }, false},
		{"api_key", "bearer token, for a server that asks for one; api_key_env is the safer way", kindText, nil,
			func(c Config) string { return shownKey(c.APIKey) }, false},
		{"api_key_env", "name of a variable that holds the token, so it stays out of the file", kindText, nil,
			func(c Config) string { return c.APIKeyEnv }, false},
		{"model", "model id; empty takes the first one the server lists", kindText, nil,
			func(c Config) string { return c.Model }, false},
		{"thinking_level", "how deeply the model thinks before answering", kindText,
			[]string{"off", "minimal", "low", "medium", "high", "xhigh", "max"},
			func(c Config) string { return c.ThinkingLevel }, true},
		{"max_tokens", "tokens per answer; empty leaves the answer the room the window has left", kindCount, nil,
			func(c Config) string { return count(c.MaxTokens) }, true},
		{"max_turns", "round-trips one ask may take before it is stopped as runaway; 0 for no limit", kindCount, nil,
			func(c Config) string { return count(c.MaxTurns) }, true},
		{"context.safety_margin", "tokens kept free for the answer when no cap was asked for", kindCount, nil,
			func(c Config) string { return fmt.Sprint(c.SafetyMargin) }, true},
		{"tools.read_lines", "lines read_file returns when the model does not ask for a window", kindCount, nil,
			func(c Config) string { return fmt.Sprint(c.ReadLines) }, false},
		{"tools.exec_timeout_sec", "seconds a command may run before it is cut", kindSeconds, nil,
			func(c Config) string { return fmt.Sprint(int(c.ExecTimeout / time.Second)) }, false},
		{"stream_idle_sec", "seconds of silence before a stream counts as dead; a cold prompt needs more", kindSeconds, nil,
			func(c Config) string { return fmt.Sprint(int(c.StreamIdle / time.Second)) }, false},
		{"assume_yes", "do not ask about writes inside the project (outside it is always asked)", kindYesNo, nil,
			func(c Config) string { return yesNo(c.AssumeYes) }, true},
		{"permissions.allow", "never ask about these: a tool name, or exec:go test; a new one is added", kindList, nil,
			func(c Config) string { return strings.Join(c.Allow, ", ") }, false},
		{"keep_sessions", "how long a saved conversation survives", kindHours, nil,
			func(c Config) string { return hours(c.KeepSessions) }, false},
		{"tools.keep_command_logs", "how long a cut command's full output is kept", kindHours, nil,
			func(c Config) string { return hours(c.KeepCommandLogs) }, false},
		{"tools.command_log_dir", "where that output goes; empty is under the home folder", kindText, nil,
			func(c Config) string { return c.CommandLogDir }, false},
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

// Setting is one knob as a screen shows it: key, purpose, current value, default, and who set it
type Setting struct {
	Key     string
	Note    string
	Value   string
	Default string
	From    string
	Options []string
	Switch  bool // on or off: the screen toggles it
	List    bool // a list: a new entry is added to what is there
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
		list = append(list, Setting{Key: k.key, Note: k.note, Value: value, Default: def, From: from, Options: k.options,
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
	"base_url":       "base-url",
	"api_key":        "api-key",
	"model":          "model",
	"max_turns":      "max-turns",
	"max_tokens":     "max-tokens",
	"thinking_level": "thinking",
}
var envFor = map[string]string{
	"base_url": "LOCAL_LLM_URL",
	"api_key":  "LOCAL_LLM_API_KEY",
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

// shownKey keeps a token out of the screen that lists settings, though the file itself may hold it
func shownKey(key string) string {
	if key == "" {
		return ""
	}
	return "set (not shown)"
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
