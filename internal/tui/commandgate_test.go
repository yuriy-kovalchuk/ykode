package tui

import (
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// the waiting rule has one owner: the table says which commands wait, and the gate holds them all
func TestEveryCommandThatWaitsIsHeldOutOfATurn(t *testing.T) {
	for _, spec := range agent.Commands() {
		if !spec.Waits {
			continue
		}
		m, _ := pickerModel(t)
		m.running = true
		m.input.SetValue(spec.Name)
		m.submit()
		if m.picker != nil {
			t.Errorf("%s opened an overlay over a turn that is running", spec.Name)
		}
		if !strings.Contains(stripCodes(m.View()), "waits for the turn") {
			t.Errorf("%s was refused without saying why", spec.Name)
		}
	}
}

// commands that do not wait answer at once: a rule applied too broadly hides everything
func TestACommandThatDoesNotWaitAnswersMidTurn(t *testing.T) {
	for _, name := range []string{"/help", "/tools", "/ctx", "/usage", "/plan"} {
		m, _ := pickerModel(t)
		m.running = true
		m.input.SetValue(name)
		m.submit()
		if strings.Contains(stripCodes(m.View()), "waits for the turn") {
			t.Errorf("%s was held behind a turn that it does not touch", name)
		}
	}
}

// a command nobody described is not assumed harmless: the gate asks the table, not the name
func TestACommandThatWaitsIsNeverReportedSafe(t *testing.T) {
	for _, spec := range agent.Commands() {
		for _, spelled := range []string{spec.Name, spec.Also} {
			if spelled == "" {
				continue
			}
			if spec.Waits && agent.CommandIsSafeDuringATurn(spelled) {
				t.Errorf("%s waits for the turn and the lookup calls it safe", spelled)
			}
			if !spec.Waits && !agent.CommandIsSafeDuringATurn(spelled) {
				t.Errorf("%s does not wait and the lookup holds it anyway", spelled)
			}
		}
	}
}

// leaving over a running turn cancels that turn rather than racing it or waiting for it
func TestTheExitCommandCancelsARunningTurnRatherThanRacingIt(t *testing.T) {
	m, _ := pickerModel(t)
	cancelled := false
	m.cancel = func() { cancelled = true }
	m.running = true
	m.input.SetValue("/exit")
	m.submit()
	if !cancelled {
		t.Error("leaving over a running turn did not cancel it")
	}
}
