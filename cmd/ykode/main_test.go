package main

import (
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/config"
)

func TestTheScreenIsTheOnlyReaderOfStdin(t *testing.T) {
	if !readsStdinLineByLine(config.Config{}) {
		t.Error("the line prompt should read stdin")
	}
	if readsStdinLineByLine(config.Config{TUI: true}) {
		t.Error("--tui would put a line reader next to the screen and eat the typed prompt")
	}
}
