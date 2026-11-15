package config

import (
	"strings"
	"testing"

	"github.com/yuriy-kovalchuk/ykode/internal/home"
)

// The screen lists keys from one table and the loader accepts keys from another, and the loader is
// strict about the difference. So a knob the screen will happily write can be one the next start refuses
// to read: the program would write a settings file that makes itself unstartable.
func TestEverySettingTheScreenWritesIsOneTheLoaderReads(t *testing.T) {
	fakeHome(t)
	root := t.TempDir() // no .ykode here, so each change lands in the user's own file
	for _, k := range knobs() {
		value := sampleFor(k)
		if value == "" {
			t.Fatalf("%s: no sample value for its kind, so the check skipped it", k.key)
		}
		if _, _, err := Set(root, k.key, value); err != nil {
			t.Errorf("%s: Set refused the value this table itself describes (%q): %v", k.key, value, err)
			continue
		}
		if _, err := Load([]string{"--root", root}); err != nil {
			t.Errorf("%s: the file Set wrote cannot be read back: %v", k.key, err)
		}
		if k.kind == kindList {
			continue // a list is not taken back through Set: it has no built-in to fall to, by design
		}
		if _, _, err := Set(root, k.key, "unset"); err != nil {
			t.Errorf("%s: unsetting the key it just wrote failed: %v", k.key, err)
		}
		if _, err := Load([]string{"--root", root}); err != nil {
			t.Errorf("%s: the file left after taking the key back cannot be read: %v", k.key, err)
		}
	}
}

// The live column is a column of the same table, not a second list of the same keys: a key that is
// live but not listed would be applied to a running session and never shown, and a listed key left out
// of the column is a change that waits for a restart in silence.
func TestTheLiveKnobsAreKeysFromTheSameList(t *testing.T) {
	live := LiveKnobs()
	listed := map[string]bool{}
	for _, k := range knobs() {
		listed[k.key] = true
	}
	for key := range live {
		if !listed[key] {
			t.Errorf("%s is applied to a running session but is not in the list the screen prints", key)
		}
	}
	for _, k := range knobs() {
		if k.live != live[k.key] {
			t.Errorf("%s: the live column and the list disagree", k.key)
		}
	}
}

// sampleFor is a value of the kind the table declares, so the test asks about the key and not the value.
func sampleFor(k knob) string {
	switch k.kind {
	case kindText:
		if len(k.options) > 0 {
			return k.options[0]
		}
		return "some-value"
	case kindCount:
		return "5"
	case kindSeconds:
		return "5"
	case kindYesNo:
		return "on"
	case kindHours:
		return "5h"
	case kindList:
		return "exec:go test"
	}
	return ""
}

// A settings file the loader cannot read must name the file and the key rather than fail as a wall of
// yaml: this is the one error a person is most likely to meet by editing the file ykode wrote.
func TestASettingsFileThatCannotBeReadNamesTheFileAndTheKey(t *testing.T) {
	fakeHome(t)
	writeSettings(t, home.Settings(), "max_turns: plenty\n")
	_, err := Load(nil)
	if err == nil {
		t.Fatal("a count that is not a count was accepted")
	}
	for _, want := range []string{"settings.yaml", "max_turns"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name %s: %v", want, err)
		}
	}
}
