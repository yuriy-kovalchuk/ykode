package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// firstAskChars is how much of an ask a listing shows before saying there is more.
const firstAskChars = 70

// Conversation is one saved session as a listing shows it: enough to pick it out again by what it
// was about rather than by its id.
type Conversation struct {
	ID       string
	Used     time.Time
	Messages int
	Root     string
	First    string
}

// Describe summarises the saved conversations, newest first. One with nothing to resume is left out:
// a cleared conversation, a half-written file, an empty run. A file that cannot be read at all does
// not sink the listing either - it is one conversation among many.
func Describe(dir string, limit int) ([]Conversation, error) {
	names, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names))) // the ids are stamped with the run time
	if limit > 0 && len(names) > limit {
		names = names[:limit]
	}
	list := make([]Conversation, 0, len(names))
	for _, name := range names {
		c, err := describe(name)
		if err != nil || c.Messages == 0 {
			continue
		}
		list = append(list, c)
	}
	return list, nil
}

// describe reads one conversation's shape without holding it in memory: its project, how many
// messages a resume would read back, and the ask a resume would start from.
func describe(path string) (Conversation, error) {
	f, err := os.Open(path)
	if err != nil {
		return Conversation{}, err
	}
	defer f.Close()

	c := Conversation{ID: strings.TrimSuffix(filepath.Base(path), ".jsonl")}
	if info, err := f.Stat(); err == nil {
		c.Used = info.ModTime()
	}
	reader := bufio.NewReader(f)
	first := true // only the very first line can name the project
	for {
		line, err := reader.ReadString('\n')
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			describeLine(&c, trimmed, first)
			first = false
		}
		if errors.Is(err, io.EOF) {
			return c, nil
		}
		if err != nil {
			return c, err
		}
	}
}

func describeLine(c *Conversation, line string, first bool) {
	if first {
		var h header
		if json.Unmarshal([]byte(line), &h) == nil && h.Root != "" {
			c.Root = h.Root
			return
		}
	}
	var b boundary
	if json.Unmarshal([]byte(line), &b) == nil && b.Boundary != "" {
		c.Messages, c.First = 0, "" // what came before the mark no longer counts
		for _, kept := range b.Kept {
			note(c, kept)
		}
		return
	}
	var m llm.Message
	if json.Unmarshal([]byte(line), &m) != nil {
		return // a half-written tail: the shape of what is whole still stands
	}
	note(c, m)
}

// note adds one message to what the listing knows about a conversation.
func note(c *Conversation, m llm.Message) {
	c.Messages++
	if c.First == "" && m.Role == "user" {
		c.First = firstAsk(m.Content)
	}
}

// firstAsk flattens an ask into one line of a listing.
func firstAsk(ask string) string {
	ask = strings.Join(strings.Fields(ask), " ")
	if runes := []rune(ask); len(runes) > firstAskChars {
		return string(runes[:firstAskChars]) + "…"
	}
	return ask
}
