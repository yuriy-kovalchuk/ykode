package tools

import "testing"

// The file tools' scope and the command tool ask the same question - how much of this budget may be
// filled - and used to answer it with two copies of the same arithmetic. One function answers it now, so
// a change to the slack or the floor cannot reach one of them and not the other.
func TestTheByteCeilingsAreOneArithmetic(t *testing.T) {
	for _, budget := range []int{0, -5, 10, 100, 700, 12000, 51200} {
		want := resultCap(budget)
		if want < minAimBytes {
			t.Errorf("budget %d: a ceiling of %d leaves nothing to cut into", budget, want)
		}
		if got := (scope{budget: budget}).aim(); got != want {
			t.Errorf("budget %d: the file tools aim at %d, the one arithmetic says %d", budget, got, want)
		}
		if got := (Exec{MaxBytes: budget}).aim(); got != want {
			t.Errorf("budget %d: the command tool aims at %d, the one arithmetic says %d", budget, got, want)
		}
	}
	if resultCap(0) != resultCap(defaultResultBytes) {
		t.Error("an unset budget does not fall to the default the rest of the package uses")
	}
}

// A ceiling is a ceiling: a tool that fills to the aim must still fit inside the budget it was given,
// with room left over for the note about the cut.
func TestAnAimedResultFitsItsBudget(t *testing.T) {
	for _, budget := range []int{1000, 12000, 51200} {
		if aim := resultCap(budget); aim+markerOverhead > budget {
			t.Errorf("budget %d leaves %d filled and %d for the note: over the budget", budget, aim, markerOverhead)
		}
	}
}
