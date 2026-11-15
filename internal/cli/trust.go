package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/config"
)

// ReportHeld says what a project's settings file asked for and did not get. With a person at the
// keyboard it offers to trust the project; a script only gets the warning. It reports whether the
// project was trusted, which is when the caller reads the settings again.
//
// The lines it prints go to stderr, which for a warning about a security decision is where they belong:
// a message that does not land is a warning that was not given, so the writes are checked and a failure
// to say anything at all is said about, on the same stream, as best it can be.
func ReportHeld(cfg config.Config, in io.Reader, interactive bool, out io.Writer) (bool, error) {
	if len(cfg.HeldKeys) == 0 {
		return false, nil
	}
	// one place keeps the first failure, so a stderr that has stopped taking text stops the question too
	var failed error
	say := func(format string, args ...any) {
		if _, err := fmt.Fprintf(out, format, args...); err != nil && failed == nil {
			failed = err
		}
	}
	say("ykode: %s sets keys that ykode applies only to a project you trust:\n", cfg.ProjectSettings)
	for _, line := range cfg.HeldValues {
		say("  %s\n", line)
	}
	if !interactive {
		say("ykode: those were not applied, and there is nobody here to ask; the project's other settings were\n")
		return false, nil
	}
	// The question is the write whose failure means something: with nowhere to put it, asking is not
	// possible, and an answer never given must not be read as a yes or a no.
	say("ykode: trust this project and use its settings? [y/N] ")
	if failed != nil {
		fmt.Fprintf(os.Stderr, "ykode: cannot ask about %s: %v - not trusting it\n", cfg.ProjectSettings, failed)
		return false, nil
	}
	// an empty answer - Enter, or a stdin nobody is answering - is a no
	answer, _ := bufio.NewReader(in).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		say("ykode: not trusting it: the keys above were not applied; the project's other settings were\n")
		return false, nil
	}
	if err := config.TrustProject(cfg.Root, cfg.ProjectHash); err != nil {
		return false, err
	}
	say("ykode: trusting this project; its settings are applied from now on\n")
	return true, nil
}
