package size

import "testing"

func TestAByteCountReadsLikeAFileListing(t *testing.T) {
	for _, want := range []struct {
		n    int
		text string
	}{{0, "0 B"}, {640, "640 B"}, {1024, "1.0 KB"}, {2150, "2.1 KB"}} {
		if got := Text(want.n); got != want.text {
			t.Errorf("Text(%d) = %s, want %s", want.n, got, want.text)
		}
	}
}
