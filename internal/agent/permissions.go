package agent

import (
	"encoding/json"
	"strings"
	"sync"
)

// permissions remembers what the user agreed to keep allowing. It is per run unless the same
// entries are put in settings.json, and it is deliberately narrow: a tool, or a command's words.
type permissions struct {
	mu      sync.Mutex // an "always" answered by one parallel call is read by the others
	allowed map[string]bool
}

func newPermissions(seeds []string) *permissions {
	p := &permissions{allowed: map[string]bool{}}
	for _, seed := range seeds {
		if key := entryKey(seed); key != "" {
			p.allowed[key] = true
		}
	}
	return p
}

// entryKey is one allow entry in its canonical form: "edit_file", or "exec:go test". A chained
// command is refused as an entry: whoever wrote it meant a second command to run without asking.
func entryKey(entry string) string {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return ""
	}
	tool, rest, isCommand := strings.Cut(entry, ":")
	tool = strings.ToLower(strings.TrimSpace(tool))
	if !isCommand {
		return tool
	}
	if chainable(rest) {
		return ""
	}
	words := strings.Fields(rest)
	if len(words) == 0 {
		return "" // "exec:" on its own would stand for every command there is
	}
	return tool + ":" + strings.Join(words, " ")
}

// commandOf is the shell line behind an exec_command call, "" when the arguments are unreadable.
func commandOf(args string) string {
	var in struct {
		Command string `json:"command"`
	}
	if json.Unmarshal([]byte(args), &in) != nil {
		return ""
	}
	return in.Command
}

// callKey is what a call would be allowed by exactly. A line that chains, substitutes or
// redirects has no key: what runs is not what its opening words promise.
func callKey(name, args string) string {
	if name != "exec_command" {
		return name
	}
	command := commandOf(args)
	if command == "" || chainable(command) {
		return ""
	}
	return entryKey("exec:" + command)
}

// chainable says whether a shell line does more than run one command: another command, a
// substitution, or a file opened by the shell instead of by a tool.
func chainable(command string) bool {
	return strings.ContainsAny(command, ";|&<>`\n\r") || strings.Contains(command, "$(")
}

// allows says whether this call is already covered. An entry may be shorter than the command, so
// "exec:ls" covers "ls -la src"; it never reaches past the words it was written with.
func (p *permissions) allows(name, args string) bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if name != "exec_command" {
		return p.allowed[name]
	}
	command := commandOf(args)
	if command == "" || chainable(command) {
		return false
	}
	words := strings.Fields(command)
	for entry := range p.allowed {
		tool, prefix, isCommand := strings.Cut(entry, ":")
		if tool != "exec" || !isCommand {
			continue
		}
		if startsWithWords(words, strings.Fields(prefix)) {
			return true
		}
	}
	return false
}

// startsWithWords says whether want is the opening words of words.
func startsWithWords(words, want []string) bool {
	if len(want) > len(words) {
		return false
	}
	for i, w := range want {
		if words[i] != w {
			return false
		}
	}
	return true
}

// add remembers a call for the rest of the run. What is remembered is the whole line the user
// was shown, never a shortened verb: only settings.json may write a broad entry on purpose.
func (p *permissions) add(name, args string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if k := callKey(name, args); k != "" {
		p.allowed[k] = true
	}
}
