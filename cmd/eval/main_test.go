package main

import (
	"slices"
	"testing"
)

func TestYkodeFlagsNeedNoDoubleDash(t *testing.T) {
	opts, rest, err := parseArgs([]string{"--only", "fix", "--base-url", "http://h/v1", "--repeat=2", "--keep", "--no-think"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.only != "fix" || opts.repeat != 2 || !opts.keep {
		t.Errorf("eval flags = %+v", opts)
	}
	if want := []string{"--base-url", "http://h/v1", "--no-think"}; !slices.Equal(rest, want) {
		t.Errorf("ykode got %v, want %v", rest, want)
	}
}

func TestARepeatBelowOneIsRefused(t *testing.T) {
	if _, _, err := parseArgs([]string{"--repeat", "0"}); err == nil {
		t.Error("--repeat 0 would run nothing and report a pass")
	}
}
