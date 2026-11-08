package audit

import "regexp"

// masked stands in for a secret in the log.
const masked = "[masked]"

// secretRules are what must never reach the log in clear text: a name with its value, a bearer
// token, a password passed as a flag, the shapes real API tokens have, and a key file pasted into
// a diff. They run in that order, so a specific shape masks itself before a general one looks.
// value is what follows a secret's name: a quoted string, or an unquoted run that stops at JSON's
// and a shell's separators instead of at a space alone.
const value = `("[^"]*"|'[^']*'|[^\s,}&|;<]+)`

var secretRules = []struct {
	re   *regexp.Regexp
	keep string // "$1[masked]" keeps the name and hides the value; masked alone hides the lot
}{
	{regexp.MustCompile(`(?i)(authorization\s*[:=]\s*(?:bearer|basic|token)\s+)[^\s,}&|;<"']+`), "$1" + masked},
	{regexp.MustCompile(`(?i)([a-z0-9_]*(?:key|token|secret|passwd|password)[a-z0-9_]*["']?\s*[:=]\s*)` + value), "$1" + masked},
	{regexp.MustCompile(`(?i)(--?[a-z-]*(?:password|passwd|token|secret|api[_-]?key)[a-z-]*[= ])` + value), "$1" + masked},
	{regexp.MustCompile(`\b(?:sk|ghp|gho|ghu|ghs|glpat)[-_][A-Za-z0-9]{6,}\b|\bxox[abprs]-[A-Za-z0-9-]{6,}\b|\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`), masked},
	{regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.+?-----END [A-Z ]*PRIVATE KEY-----`), masked},
}

func redact(s string) string {
	for _, rule := range secretRules {
		s = rule.re.ReplaceAllString(s, rule.keep)
	}
	return s
}

func redactAll(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = redact(v)
	}
	return out
}
