package session

import "strings"

// Environment marks the note added to the end of an ask: date and git state, never part of the ask
const (
	EnvironmentOpen  = "\n\n<environment>\n"
	EnvironmentClose = "\n</environment>"
)

// WithoutEnvironment is an ask as it was typed.
func WithoutEnvironment(ask string) string {
	text, _ := SplitEnvironment(ask)
	return text
}

// SplitEnvironment separates an ask from the note at its end, which is "" when it has none.
func SplitEnvironment(ask string) (text, note string) {
	if !strings.HasSuffix(ask, EnvironmentClose) {
		return ask, ""
	}
	at := strings.LastIndex(ask, EnvironmentOpen)
	if at < 0 {
		return ask, ""
	}
	return ask[:at], ask[at+len(EnvironmentOpen) : len(ask)-len(EnvironmentClose)]
}
