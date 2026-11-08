package agent

import (
	"context"
	"fmt"
	"strings"
)

// outOfTurns turns the reading already done into an answer instead of an error and an empty reply.
func (a *Agent) outOfTurns(ctx context.Context) error {
	a.tell(fmt.Sprintf("used all %d tool rounds: asking for an answer with no tools left to call", a.opts.MaxTurns))
	err := a.forceAnswer(ctx, "No tools now. Answer in text from what you have already read, and say what you could not confirm.")
	if err == errNoForcedAnswer {
		return fmt.Errorf("reached --max-turns (%d) before the model answered", a.opts.MaxTurns)
	}
	return err
}

// errNoForcedAnswer is a forced answer that came back empty.
var errNoForcedAnswer = fmt.Errorf("the model gave no answer when asked for one with no tools left")

// forceAnswer asks once more with no tools offered, so whatever the run has read becomes an answer.
func (a *Agent) forceAnswer(ctx context.Context, instruction string) error {
	// an assistant message would be continued by the server as prefill, so this goes in the user's voice
	a.modelNote(instruction)
	req, err := a.request(false)
	if err != nil {
		return err
	}
	req.Tools, req.ToolChoice = nil, ""
	msg, finish, err := a.chat(ctx, req)
	if err != nil {
		if ctx.Err() != nil {
			a.note("(the user cancelled this request)")
			return nil
		}
		return err
	}
	// nothing will run this call, and a call with no reply makes the next request invalid
	if len(msg.ToolCalls) > 0 {
		msg.ToolCalls = nil
		a.tell("the model asked for a tool after being told there were none; only its text is kept")
	}
	a.add(msg)
	if finish == finishLength {
		a.tell("the response hit max_tokens and may be incomplete")
	}
	if strings.TrimSpace(msg.Content) == "" {
		a.note("(stopped without a final answer)")
		return errNoForcedAnswer
	}
	a.tell("answered from what it had; the turn budget or a repeating call stopped it, not the answer")
	return nil
}
