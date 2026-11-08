package tools

import (
	"context"
	"encoding/json"
	"fmt"
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
		{strings.Repeat("b", defaultResultBytes), strings.Repeat("b", defaultResultBytes)},
	}
	for _, tc := range cases {
		if got := FormatResult(tc.in, defaultResultBytes); got != tc.want {
			t.Errorf("FormatResult(%q, defaultResultBytes) = %q, want %q", shortened(tc.in), got, tc.want)
		}
	}
}

func TestFormatResultCountsExactlyWhatItDropped(t *testing.T) {
	body := "FIRST " + strings.Repeat("x", defaultResultBytes) + " LAST"

	got := FormatResult(body, defaultResultBytes)
	if len(got) > defaultResultBytes {
		t.Fatalf("result is %d bytes, over the %d cap", len(got), defaultResultBytes)
	}
	if !strings.HasPrefix(got, "FIRST ") || !strings.HasSuffix(got, " LAST") {
		t.Fatal("a long log matters at both ends; both ends must survive")
	}

	// read the promise back out: head + omitted + tail must equal the original byte count
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
	if huge := FormatResult(strings.Repeat("z", 10<<20), defaultResultBytes); len(huge) > defaultResultBytes {
		t.Fatalf("a 10 MB result came back at %d bytes, over the %d cap", len(huge), defaultResultBytes)
	}
}

var omittedMarker = regexp.MustCompile(`(?s)^(.*)\[\.\.\. (\d+) bytes omitted \.\.\.\]\n\n(.*)$`)

func TestCutsNeverSplitARune(t *testing.T) {
	body := strings.Repeat("héllo 🌍 ok", 4000)
	if !utf8.ValidString(body) {
		t.Fatal("fixture must be valid UTF-8")
	}
	for _, n := range []int{1, 2, 3, 5, 7, 13, 64, defaultResultBytes / 2} {
		if head := cutUTF8(body, n); len(head) > n || !utf8.ValidString(head) {
			t.Errorf("cutUTF8(%d) = %d bytes, valid=%v", n, len(head), utf8.ValidString(head))
		}
		if tail := cutUTF8Tail(body, n); len(tail) > n || !utf8.ValidString(tail) {
			t.Errorf("cutUTF8Tail(%d) = %d bytes, valid=%v", n, len(tail), utf8.ValidString(tail))
		}
	}

	// The boundary case: a 4-byte rune sitting exactly where the cut lands.
	body = strings.Repeat("ab", defaultResultBytes/2) + "🌍" + strings.Repeat("cd", defaultResultBytes)
	if got := FormatResult(body, defaultResultBytes); !utf8.ValidString(got) {
		t.Fatal("a cut through a multi-byte rune produced invalid UTF-8")
	}
}

func shortened(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}

func TestTheResultBudgetScalesWithTheWindow(t *testing.T) {
	cases := []struct {
		ctx  int
		want int
	}{
		{0, defaultResultBytes}, // unknown window keeps the old cap
		{4096, 4800},            // a small window gets a small result
		{32768, 13104},          // ctx/10 in tokens, four bytes each
		{131072, 24000},         // clamped: a huge window does not mean a huge result
	}
	for _, c := range cases {
		if got := ResultBytesFor(c.ctx); got != c.want {
			t.Errorf("ResultBytesFor(%d) = %d, want %d", c.ctx, got, c.want)
		}
	}
	if small, big := ResultBytesFor(8192), ResultBytesFor(65536); small >= big {
		t.Errorf("budget %d for 8k is not below %d for 64k", small, big)
	}
}

func TestTheConfiguredBudgetReachesTheTools(t *testing.T) {
	files := map[string]string{}
	for i := 0; i < 400; i++ {
		files[fmt.Sprintf("dir/file%03d.txt", i)] = "x"
	}
	root, dir := treeFixture(t, files)
	registry := BuiltIn(root, dir, Options{MaxResultBytes: 4000})

	find, ok := registry.Get("find_files")
	if !ok {
		t.Fatal("find_files is not registered")
	}
	out, err := find.Run(context.Background(), json.RawMessage(`{"pattern":"*.txt","limit":400}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) > 4000 {
		t.Fatalf("find_files returned %d bytes, want the configured 4000 cap: %s", len(out), cutUTF8(out, 120))
	}
	if !strings.Contains(out, "byte budget") {
		t.Fatalf("no note about the budget:\n%s", cutUTF8(out, 200))
	}
}
