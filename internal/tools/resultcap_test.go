package tools

import "testing"

// the file tools and the command tool ask the same question, so one function answers it for both
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

// a ceiling is a ceiling: filling to the aim must still fit the budget, the note included
func TestAnAimedResultFitsItsBudget(t *testing.T) {
	for _, budget := range []int{1000, 12000, 51200} {
		if aim := resultCap(budget); aim+markerOverhead > budget {
			t.Errorf("budget %d leaves %d filled and %d for the note: over the budget", budget, aim, markerOverhead)
		}
	}
}
