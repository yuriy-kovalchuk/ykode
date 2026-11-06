package tools

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFormatResultPassesSmallResultsThrough(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", noOutput},
		{"   \n\t ", noOutput},
		{"  one line\n", "one line"},
		{strings.Repeat("b", maxResultBytes), strings.Repeat("b", maxResultBytes)},
	}
	for _, tc := range cases {
		if got := FormatResult(tc.in); got != tc.want {
			t.Errorf("FormatResult(%q) = %q, want %q", shortened(tc.in), got, tc.want)
		}
	}
}

func TestFormatResultCountsExactlyWhatItDropped(t *testing.T) {
	body := "FIRST " + strings.Repeat("x", maxResultBytes) + " LAST"

	got := FormatResult(body)
	if len(got) > maxResultBytes {
		t.Fatalf("result is %d bytes, over the %d cap", len(got), maxResultBytes)
	}
	if !strings.HasPrefix(got, "FIRST ") || !strings.HasSuffix(got, " LAST") {
		t.Fatal("a long log matters at both ends; both ends must survive")
	}

	// Read the promise back out of the result: head + omitted + tail must be the
	// original byte count, whatever the cutter decided to keep.
	m := omittedMarker.FindStringSubmatch(got)
	if m == nil {
		t.Fatalf("no omission marker in:\n%s", shortened(got))
	}
	head := strings.TrimSuffix(m[1], "\n\n")
	tail := strings.TrimPrefix(m[3], "\n\n")
	dropped, err := strconv.Atoi(m[2])
	if err != nil {
		t.Fatalf("marker count %q is not a number: %v", m[2], err)
	}
	if dropped <= 0 {
		t.Fatalf("marker claims %d bytes omitted while bytes are gone", dropped)
	}
	if head != "" && !strings.HasPrefix(body, head) {
		t.Fatal("the head must be the real beginning, not invented text")
	}
	if tail != "" && !strings.HasSuffix(body, tail) {
		t.Fatal("the tail must be the real ending")
	}
	if len(head)+dropped+len(tail) != len(body) {
		t.Fatalf("marker says %d omitted, really %d", dropped, len(body)-len(head)-len(tail))
	}

	// The cap is a promise to the agent, so it has to hold for any size.
	if huge := FormatResult(strings.Repeat("z", 10<<20)); len(huge) > maxResultBytes {
		t.Fatalf("a 10 MB result came back at %d bytes, over the %d cap", len(huge), maxResultBytes)
	}
}

var omittedMarker = regexp.MustCompile(`(?s)^(.*)\[\.\.\. (\d+) bytes omitted \.\.\.\]\n\n(.*)$`)

func TestCutsNeverSplitARune(t *testing.T) {
	body := strings.Repeat("héllo 🌍 ok", 4000)
	if !utf8.ValidString(body) {
		t.Fatal("fixture must be valid UTF-8")
	}
	for _, n := range []int{1, 2, 3, 5, 7, 13, 64, maxResultBytes / 2} {
		if head := cutUTF8(body, n); len(head) > n || !utf8.ValidString(head) {
			t.Errorf("cutUTF8(%d) = %d bytes, valid=%v", n, len(head), utf8.ValidString(head))
		}
		if tail := cutUTF8Tail(body, n); len(tail) > n || !utf8.ValidString(tail) {
			t.Errorf("cutUTF8Tail(%d) = %d bytes, valid=%v", n, len(tail), utf8.ValidString(tail))
		}
	}

	// The boundary case: a 4-byte rune sitting exactly where the cut lands.
	body = strings.Repeat("ab", maxResultBytes/2) + "🌍" + strings.Repeat("cd", maxResultBytes)
	if got := FormatResult(body); !utf8.ValidString(got) {
		t.Fatal("a cut through a multi-byte rune produced invalid UTF-8")
	}
}

func shortened(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}
