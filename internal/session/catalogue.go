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

// Conversation is one saved session as a listing shows it: enough to pick it out by what it was about
type Conversation struct {
	ID       string
	Used     time.Time
	Messages int
	Root     string
	First    string
}

// Describe summarises the saved conversations, newest first, skipping ones with nothing to resume
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

// describe reads one conversation's shape without holding it in memory: project, size, and first ask
func describe(path string) (Conversation, error) {
	f, err := os.Open(path)
	if err != nil {
		return Conversation{}, err
	}
	// a read whose file would not close has already given its lines, and nothing unsent is to lose here
	defer func() { _ = f.Close() }()

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
			note(c, kept.Message)
		}
		return
	}
	var ch change
	if json.Unmarshal([]byte(line), &ch) == nil && (ch.Revised != nil || ch.Removed != nil || ch.BytesPerToken > 0) {
		// counted as Load applies it: a range that does not fit the conversation is left out there too
		if len(ch.Removed) == 2 && ch.Removed[0] >= 0 && ch.Removed[0] <= ch.Removed[1] && ch.Removed[1] <= c.Messages {
			c.Messages -= ch.Removed[1] - ch.Removed[0]
		}
		return // a change to what was said is not something said
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
		c.First = firstAsk(WithoutEnvironment(m.Content))
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
