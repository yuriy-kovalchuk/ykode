package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
	"github.com/yuriy-kovalchuk/ykode/internal/llm"
	"github.com/yuriy-kovalchuk/ykode/internal/session"
)

// resume puts the conversation a run came back to on the screen, above the prompt that continues it,
// and returns the asks it holds so the earlier prompts stay walkable.
func resume(log *transcript, id string, messages []llm.Message) []string {
	if len(messages) < 2 {
		return nil // only the system prompt: nothing was carried over
	}
	where := "an earlier conversation"
	if id != "" {
		where = id
	}
	log.notice(fmt.Sprintf("came back to %s: %d messages, scroll up to read them", where, len(messages)-1))
	return log.replay(messages)
}

// replay draws the conversation a run came back to and returns its asks, so the earlier prompts are
// still walkable. The model already holds these turns; a blank screen would say otherwise.
func (t *transcript) replay(messages []llm.Message) []string {
	var asks []string
	var open []llm.ToolCall // the calls of the assistant message being answered
	for _, m := range messages {
		switch m.Role {
		case "user":
			recap, folded := strings.CutPrefix(m.Content, agent.FoldMark)
			if folded {
				t.summary(strings.TrimSpace(recap))
				continue
			}
			if note, ours := strings.CutPrefix(m.Content, agent.NoteMark); ours {
				t.notice(note) // what ykode told the model in the user's place is not something the user asked
				continue
			}
			// the hour was never saved, so a carried-over ask shows no clock; the note ykode added to the
			// end of it is not what was typed
			ask := session.WithoutEnvironment(m.Content)
			t.user(ask, time.Time{})
			asks = append(asks, ask)
		case "assistant":
			if thought := strings.TrimSpace(m.Reasoning); thought != "" {
				t.thinking(thought) // what the model thought is saved with what it said
			}
			if text := strings.TrimSpace(m.Content); text != "" {
				t.answer(text)
			}
			open = m.ToolCalls
		case "tool":
			// a call the trimming took away still has an answer the reader should see
			if len(open) == 0 {
				t.toolEnd(m.Name, "", m.Content)
				continue
			}
			call := open[0]
			open = open[1:]
			args := string(call.Function.Arguments)
			t.toolStart(call.Function.Name, args) // the call row says what it touched, as a live one would
			t.toolEnd(call.Function.Name, args, replayed(m.Content))
		}
	}
	return asks
}

// replayed leaves out what the run itself wrote into a result: the shortening note says the text is gone,
// and whoever reads the saved conversation has the call without being shown the note.
func replayed(content string) string {
	if strings.HasPrefix(content, agent.ShrinkMark) {
		return ""
	}
	return content
}
