package agent

import (
	"encoding/json"
	"strings"
)

// permissions remembers what the user agreed to keep allowing. It is per run unless the same
// entries are put in settings.json, and it is deliberately narrow: a tool, or a command prefix.
type permissions struct {
	allowed map[string]bool
}

func newPermissions(seeds []string) *permissions {
	p := &permissions{allowed: map[string]bool{}}
	for _, seed := range seeds {
		if key := normalizeKey(seed); key != "" {
			p.allowed[key] = true
		}
	}
	return p
}

// key names what a call would have to be allowed by: "edit_file", or "exec:go test".
// The command key keeps the first two words, which is where the verb of a command lives.
func key(name, args string) string {
	if name != "exec_command" {
		return name
	}
	var in struct {
		Command string `json:"command"`
	}
	if json.Unmarshal([]byte(args), &in) != nil || in.Command == "" {
		return "" // an unparseable command is not something to remember approval for
	}
	return normalizeKey("exec:" + in.Command)
}

// normalizeKey trims a command down to its verb and lowercases a tool name.
func normalizeKey(entry string) string {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return ""
	}
	tool, rest, isPrefix := strings.Cut(entry, ":")
	tool = strings.ToLower(strings.TrimSpace(tool))
	if !isPrefix {
		return tool
	}
	words := strings.Fields(rest)
	if len(words) > 2 {
		words = words[:2]
	}
	return tool + ":" + strings.Join(words, " ")
}

// allows says whether this call is already covered.
func (p *permissions) allows(name, args string) bool {
	if p == nil {
		return false
	}
	return p.allowed[key(name, args)]
}

// add remembers a call for the rest of the run.
func (p *permissions) add(name, args string) {
	if p == nil {
		return
	}
	if k := key(name, args); k != "" {
		p.allowed[k] = true
	}
}
