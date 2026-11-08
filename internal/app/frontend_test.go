package app

import (
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/config"
)

func TestTheScreenIsTheOnlyReaderOfStdin(t *testing.T) {
	if !readsStdinLineByLine(config.Config{}) {
		t.Error("the line prompt should read stdin")
	}
	if readsStdinLineByLine(config.Config{TUI: true}) {
		t.Error("the full-screen session would get a line reader next to it, eating the typed prompt")
	}
}
