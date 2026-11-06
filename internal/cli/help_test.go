package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

func TestHelpListsEveryCommand(t *testing.T) {
	for _, want := range []string{"/help", "/usage", "/reset", "/exit"} {
		if !strings.Contains(helpText, want) {
			t.Errorf("help is missing %q:\n%s", want, helpText)
		}
	}
}

func TestCommandsReachTheirHandlers(t *testing.T) {
	var out, log bytes.Buffer
	r := NewREPL(agent.New(nil, nil, "m", agent.Options{}, nil), NewLineSource(strings.NewReader("")), &out, &log)
	for _, line := range []string{"/help", "/usage", "/reset", "/sideways"} {
		if quit, err := r.command(line); quit || err != nil {
			t.Fatalf("%s: quit=%v err=%v", line, quit, err)
		}
	}
	printed := out.String() + log.String()
	for _, want := range []string{"commands:", "usage:", "history cleared", "unknown command"} {
		if !strings.Contains(printed, want) {
			t.Errorf("%q was never printed:\n%s", want, printed)
		}
	}
}
