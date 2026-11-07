package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/yuriy-kovalchuk/ykode/internal/config"
)

// reportHeld says what a project's settings file asked for and did not get. With a person at the
// keyboard it offers to trust the project; a script only gets the warning. It reports whether the
// project was trusted, which is when the caller reads the settings again.
func reportHeld(cfg config.Config, in io.Reader, interactive bool, out io.Writer) (bool, error) {
	if len(cfg.HeldKeys) == 0 {
		return false, nil
	}
	fmt.Fprintf(out, "ykode: %s sets %s, which ykode applies only to a project you trust\n",
		cfg.ProjectSettings, strings.Join(cfg.HeldKeys, ", "))
	if !interactive {
		fmt.Fprintln(out, "ykode: none of it was applied, and there is nobody here to ask")
		return false, nil
	}
	fmt.Fprint(out, "ykode: trust this project and use its settings? [y/N] ")
	// an empty answer - Enter, or a stdin nobody is answering - is a no
	answer, _ := bufio.NewReader(in).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer != "y" && answer != "yes" {
		fmt.Fprintln(out, "ykode: not trusting it, so nothing from the project was used")
		return false, nil
	}
	if err := config.TrustProject(cfg.Root); err != nil {
		return false, err
	}
	fmt.Fprintln(out, "ykode: trusting this project; its settings are applied from now on")
	return true, nil
}
