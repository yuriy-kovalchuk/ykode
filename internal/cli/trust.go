package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/config"
)

// ReportHeld says what a project's settings file asked for and did not get. With a person at the
// keyboard it offers to trust the project; a script only gets the warning. It reports whether the
// project was trusted, which is when the caller reads the settings again.
func ReportHeld(cfg config.Config, in io.Reader, interactive bool, out io.Writer) (bool, error) {
	if len(cfg.HeldKeys) == 0 {
		return false, nil
	}
	fmt.Fprintf(out, "ykode: %s sets keys that ykode applies only to a project you trust:\n", cfg.ProjectSettings)
	for _, line := range cfg.HeldValues {
		fmt.Fprintf(out, "  %s\n", line)
	}
	if !interactive {
		fmt.Fprintln(out, "ykode: those were not applied, and there is nobody here to ask; the project's other settings were")
		return false, nil
	}
	fmt.Fprint(out, "ykode: trust this project and use its settings? [y/N] ")
	// an empty answer - Enter, or a stdin nobody is answering - is a no
	answer, _ := bufio.NewReader(in).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		fmt.Fprintln(out, "ykode: not trusting it: the keys above were not applied; the project's other settings were")
		return false, nil
	}
	if err := config.TrustProject(cfg.Root, cfg.ProjectHash); err != nil {
		return false, err
	}
	fmt.Fprintln(out, "ykode: trusting this project; its settings are applied from now on")
	return true, nil
}
