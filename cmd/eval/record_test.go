package main

import (
	"strings"
	"testing"
	"time"
)

func TestAWorsePassRateIsMarkedAsARegression(t *testing.T) {
	base := runRecord{Tasks: []taskRecord{{Name: "fix-the-bug", Passed: 3, Runs: 3}, {Name: "steady", Passed: 2, Runs: 3}}}
	now := runRecord{Tasks: []taskRecord{{Name: "fix-the-bug", Passed: 1, Runs: 3}, {Name: "steady", Passed: 2, Runs: 3}, {Name: "brand-new", Passed: 1, Runs: 1}}}

	report := compare(base, now)

	for _, want := range []string{"REGRESSION fix-the-bug", "same       steady", "new        brand-new"} {
		if !strings.Contains(report, want) {
			t.Errorf("report misses %q:\n%s", want, report)
		}
	}
}

func TestATaskReportsItsPassRateAndMedians(t *testing.T) {
	rec := summarise("t", []evalResult{
		{passed: true, turns: 2, counters: map[string]int{"recall": 1}},
		{passed: false, turns: 9},
		{passed: true, turns: 3, counters: map[string]int{"recall": 1}},
	})
	if rec.Passed != 2 || rec.Runs != 3 || rec.Turns != 3 || rec.Counters["recall"] != 2 {
		t.Errorf("summary = %+v", rec)
	}
}

func TestLastIsTheNewestEarlierRun(t *testing.T) {
	dir := t.TempDir()
	older := runRecord{Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Model: "older"}
	newer := runRecord{Time: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), Model: "newer"}
	if _, err := older.save(dir); err != nil {
		t.Fatal(err)
	}
	current, err := newer.save(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := loadRun(dir, "last", current)
	if err != nil || got.Model != "older" {
		t.Errorf("last = %q, %v; want the run before this one", got.Model, err)
	}
}
