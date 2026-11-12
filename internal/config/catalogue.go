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
	kindDays
	kindList // written as a list; a new value is added to what is there
)

// knob is one thing settings.yaml can change: the key as the file spells it (dotted when it sits
// inside a section), what it is for, and the kind of value it takes.
type knob struct {
	key     string
	note    string
	kind    int
	options []string // the choices when there are few; anything of its kind fits when nil
	value   func(Config) string
}

// knobs are every setting the file can set, in the order a reader would look for them: the server,
// then the model, then the tools, then what is kept.
func knobs() []knob {
	return []knob{
		{"base_url", "the server ykode asks; a first session asks for it", kindText, nil,
			func(c Config) string { return c.BaseURL }},
		{"api_key", "bearer token, for a server that asks for one; api_key_env is the safer way", kindText, nil,
			func(c Config) string { return shownKey(c.APIKey) }},
		{"api_key_env", "name of a variable that holds the token, so it stays out of the file", kindText, nil,
			func(c Config) string { return c.APIKeyEnv }},
		{"model", "model id; empty takes the first one the server lists", kindText, nil,
			func(c Config) string { return c.Model }},
		{"thinking_level", "how deeply the model thinks before answering", kindText,
			[]string{"off", "minimal", "low", "medium", "high", "xhigh", "max"},
			func(c Config) string { return c.ThinkingLevel }},
		{"max_tokens", "tokens per answer; empty leaves the answer the room the window has left", kindCount, nil,
			func(c Config) string { return count(c.MaxTokens) }},
		{"max_turns", "round-trips one ask may take before it is stopped as runaway; 0 for no limit", kindCount, nil,
			func(c Config) string { return count(c.MaxTurns) }},
		{"context.safety_margin", "tokens kept free for the answer when no cap was asked for", kindCount, nil,
			func(c Config) string { return fmt.Sprint(c.SafetyMargin) }},
		{"tools.read_lines", "lines read_file returns when the model does not ask for a window", kindCount, nil,
			func(c Config) string { return fmt.Sprint(c.ReadLines) }},
		{"tools.search_limit", "matches search_text returns when the model does not ask for more", kindCount, nil,
			func(c Config) string { return fmt.Sprint(c.SearchLimit) }},
		{"tools.exec_timeout_sec", "seconds a command may run before it is cut", kindSeconds, nil,
			func(c Config) string { return fmt.Sprint(int(c.ExecTimeout / time.Second)) }},
		{"stream_idle_sec", "seconds of silence before a stream counts as dead; a cold prompt needs more", kindSeconds, nil,
			func(c Config) string { return fmt.Sprint(int(c.StreamIdle / time.Second)) }},
		{"verify", "ask the model to redo a memory-only answer with the tools", kindYesNo, nil,
			func(c Config) string { return yesNo(!c.NoVerify) }},
		{"assume_yes", "do not ask about writes inside the project (outside it is always asked)", kindYesNo, nil,
			func(c Config) string { return yesNo(c.AssumeYes) }},
		{"mouse", "the screen takes the mouse: the wheel scrolls, and the terminal cannot select text", kindYesNo, nil,
			func(c Config) string { return yesNo(c.Mouse) }},
		{"sandbox", "confine commands: writes only inside the project, no network", kindYesNo, nil,
			func(c Config) string { return yesNo(c.Sandbox) }},
		{"permissions.allow", "never ask about these: a tool name, or exec:go test; a new one is added", kindList, nil,
			func(c Config) string { return strings.Join(c.Allow, ", ") }},
		{"audit", "write the tool-call log at all", kindYesNo, nil,
			func(c Config) string { return yesNo(c.LogAudit) }},
		{"audit_retention_days", "days of that log to keep", kindDays, nil,
			func(c Config) string { return fmt.Sprint(int(c.KeepAudit / (24 * time.Hour))) }},
		{"keep_sessions", "how long a saved conversation and its results survive", kindHours, nil,
			func(c Config) string { return hours(c.KeepSessions) }},
		{"tools.keep_command_logs", "how long a cut command's full output is kept", kindHours, nil,
			func(c Config) string { return hours(c.KeepCommandLogs) }},
		{"tools.command_log_dir", "where that output goes; empty is under the home folder", kindText, nil,
			func(c Config) string { return c.CommandLogDir }},
	}
}

// Setting is one knob as a screen shows it: the key, what it is for, what it is now, what it would
// be with nothing set, and who set it.
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
	file := fileKeys(c)
	list := make([]Setting, 0, len(knobs()))
	for _, k := range knobs() {
		value, def := k.value(c), k.value(blank)
		from := c.where(k.key, file)
		if from == FromFile && value == def {
			from = FromDefault // a file that only repeats the built-in has not chosen anything
		}
		list = append(list, Setting{Key: k.key, Note: k.note, Value: value, Default: def, From: from, Options: k.options,
			Switch: k.kind == kindYesNo, List: k.kind == kindList})
	}
	return list
}

// where says who set a knob: a flag beats the environment, which beats a settings file, which
// beats the built-in.
func (c Config) where(key string, file map[string]bool) string {
	if flag, ok := flagFor[key]; ok && c.flagsGiven[flag] {
		return FromFlag
	}
	if name, ok := envFor[key]; ok && os.Getenv(name) != "" {
		return FromEnv
	}
	if file[key] {
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

// fileKeys are the knobs both settings files set, dotted as the catalogue names them. The files are
// read again here because the screen asks now, not when the run started.
func fileKeys(c Config) map[string]bool {
	found := map[string]bool{}
	for _, path := range []string{settingsIn(home.Dir()), projectSettingsPath(c.Root)} {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var doc yaml.Node
		if yaml.Unmarshal(raw, &doc) != nil || len(doc.Content) == 0 {
			continue
		}
		walkKeys(doc.Content[0], "", found)
	}
	return found
}

func walkKeys(node *yaml.Node, prefix string, into map[string]bool) {
	if node.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		name := prefix + node.Content[i].Value
		if node.Content[i+1].Kind == yaml.MappingNode {
			walkKeys(node.Content[i+1], name+".", into)
			continue
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

// shownKey keeps a token out of the screen that lists settings: the file may hold it, the screen
// should not repeat it.
func shownKey(key string) string {
	if key == "" {
		return ""
	}
	return "set (not shown)"
}

// ModelFromFile says the model in use was pinned by a settings file, not named by a flag or the
// environment: a flag is a request for that model, a pin in a file is only a remembered choice.
func (c Config) ModelFromFile() bool { return c.where("model", fileKeys(c)) == FromFile }
