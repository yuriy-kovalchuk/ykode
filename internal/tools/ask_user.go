package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// AskUser stops the run and puts one question, with its choices, to the person running ykode
type AskUser struct{ ask Asker }

// Question is what is asked, and the choices the model would act on.
type Question struct {
	Text    string
	Options []string
}

// Asker puts that question to a person and waits; an empty answer says there is none
type Asker interface {
	Ask(ctx context.Context, q Question) string
}

// maxAskOptions is choices one question offers: more than a screen reads at once
const maxAskOptions = 6

// noAnswer is what the model reads when the question came back empty; the run does not stop
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
	q := Question{Text: strings.TrimSpace(in.Question), Options: cleanOptions(in.Options)}
	if q.Text == "" {
		return "", errors.New("ask_user: nothing was asked; send the question written out")
	}
	if t.ask == nil {
		return noAnswer, nil // nobody was ever going to answer it
	}
	answer := strings.TrimSpace(t.ask.Ask(ctx, q))
	if answer == "" {
		return noAnswer, nil
	}
	return "their answer: " + answer, nil
}

// cleanOptions is the list as shown: one line each, nothing for a dropped choice, none over a screen
func cleanOptions(in []string) []string {
	options := make([]string, 0, min(len(in), maxAskOptions))
	for _, option := range in {
		text := strings.TrimSpace(option)
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
