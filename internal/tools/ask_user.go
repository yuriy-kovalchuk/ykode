package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// AskUser stops the run and puts one question to the person running ykode, with the choices the
// model would act on. It is for the point where guessing is more expensive than asking: which of two
// designs, which file among several, whether to do the hard-to-undo part at all.
type AskUser struct{ ask Asker }

// Question is what is asked, and the choices the model would act on.
type Question struct {
	Text    string
	Options []string
}

// Asker puts that question to a person and waits for the answer in words. An empty answer says there
// is none: nobody is watching, the screen closed, or the run was cancelled while it waited.
type Asker interface {
	Ask(ctx context.Context, q Question) string
}

const (
	askQuestionChars = 400 // what a question says; past this it is a document, not a question
	askOptionChars   = 120 // what one choice says
	askAnswerChars   = 600 // what an answer may say back
	maxAskOptions    = 6   // choices one question offers; more than a screen reads at once
)

// noAnswer is what the model reads when the question came back empty. The choice and the report of it
// are its job from there, so the run does not stop for want of an answer.
const noAnswer = "no answer; go on with the choice you think best, and say what you assumed when you report the work"

func NewAskUser(ask Asker) *AskUser { return &AskUser{ask: ask} }

func (*AskUser) Name() string { return "ask_user" }
func (*AskUser) Class() Class { return ClassSays }

func (*AskUser) Description() string {
	return "Pause the run and ask the person one question, with the choices you would act on. Ask when the project cannot answer what you need and a wrong guess costs work; ask once about a decision, not about every step. An empty answer means they have none: pick the most reasonable choice and say what you assumed."
}

func (*AskUser) NeedsConfirm() bool { return false }

func (*AskUser) Targets(json.RawMessage) ([]Target, error) { return nil, nil }

func (*AskUser) Schema() json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{
	  "type": "object",
	  "properties": {
	    "question": {"type": "string", "description": "one question, in the words they should read"},
	    "options": {
	      "type": "array",
	      "maxItems": %d,
	      "items": {"type": "string"},
	      "description": "the choices you would act on, in the order to show; leave it out to ask open"
	    }
	  },
	  "required": ["question"]
	}`, maxAskOptions))
}

type askInput struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
}

func (t *AskUser) Run(ctx context.Context, args json.RawMessage) (string, error) {
	var in askInput
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	q := Question{Text: oneLine(in.Question, askQuestionChars), Options: cleanOptions(in.Options)}
	if q.Text == "" {
		return "", errors.New("ask_user: nothing was asked; send the question written out")
	}
	if t.ask == nil {
		return noAnswer, nil // nobody was ever going to answer it
	}
	answer := oneLine(t.ask.Ask(ctx, q), askAnswerChars)
	if answer == "" {
		return noAnswer, nil
	}
	return "their answer: " + answer, nil
}

// cleanOptions is the list as it is shown: one line each, nothing in place of a choice dropped, and no
// more than a screen holds.
func cleanOptions(in []string) []string {
	options := make([]string, 0, min(len(in), maxAskOptions))
	for _, option := range in {
		text := oneLine(option, askOptionChars)
		if text == "" || len(options) == maxAskOptions {
			continue
		}
		options = append(options, text)
	}
	return options
}

// choiceShape words an ask call for a row: the question, and how many choices come with it.
func choiceShape(in askInput) string {
	question := oneLine(in.Question, describeChars)
	if n := len(cleanOptions(in.Options)); n > 0 {
		return fmt.Sprintf("%s · %d choices", question, n)
	}
	return question
}
