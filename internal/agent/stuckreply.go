package agent

import (
	"errors"
	"strings"
)

const (
	stuckLineMin   = 24 // shorter lines (a "}", "return nil") repeat in honest text
	stuckLineLimit = 10 // one line this many times in a single reply is a model going in circles
)

// errRepeating is a reply that was stopped because it kept writing the same lines.
var errRepeating = errors.New("the reply kept repeating the same lines")

// lineRepeats catches a reply stuck repeating itself, which nothing else stops before the window fills.
type lineRepeats struct {
	partial strings.Builder
	seen    map[string]int
}

func newLineRepeats() *lineRepeats { return &lineRepeats{seen: map[string]int{}} }

// add takes the next piece of text and says whether some line has now come back stuckLineLimit times.
func (w *lineRepeats) add(text string) bool {
	for text != "" {
		i := strings.IndexByte(text, '\n')
		if i < 0 {
			w.partial.WriteString(text)
			return false
		}
		w.partial.WriteString(text[:i])
		text = text[i+1:]
		line := strings.TrimSpace(w.partial.String())
		w.partial.Reset()
		if len(line) < stuckLineMin {
			continue
		}
		w.seen[line]++
		if w.seen[line] >= stuckLineLimit {
			return true
		}
	}
	return false
}
