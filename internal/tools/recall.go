package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// Recall hands back tool output that was shortened out of the conversation.
type Recall struct {
	scope
	store  ResultStore
	served map[string]bool // ids already handed over, so a second one costs a line
}

type recallInput struct {
	ID string `json:"id"`
}

func NewRecall(store ResultStore) *Recall {
	return &Recall{store: store, served: map[string]bool{}}
}

func (*Recall) Name() string { return "recall" }

func (*Recall) Description() string {
	return "Hand back the full text of an earlier tool result that was shortened to fit the context. The id comes from the summary that replaced it, e.g. r7."
}

func (*Recall) NeedsConfirm() bool { return false }

func (*Recall) Schema() json.RawMessage {
	return json.RawMessage(`{
	  "type": "object",
	  "properties": {
	    "id": {"type": "string", "description": "the result id from the summary, e.g. r7"}
	  },
	  "required": ["id"]
	}`)
}

func (t *Recall) Targets(json.RawMessage) ([]Target, error) { return nil, nil }

func (t *Recall) Run(_ context.Context, args json.RawMessage) (string, error) {
	var in recallInput
	if err := decodeArgs(args, &in); err != nil {
		return "", err
	}
	if !t.store.Saved() {
		return "", fmt.Errorf("no results are kept by this ykode, so nothing can be recalled")
	}
	text, err := t.store.Load(in.ID)
	if err != nil {
		return "", err
	}
	if t.served == nil {
		t.served = map[string]bool{}
	}
	if t.served[in.ID] {
		return fmt.Sprintf("%s was already handed over whole; it has not changed since. work from what you have", in.ID), nil
	}
	t.served[in.ID] = true

	header := fmt.Sprintf("saved result %s: %d bytes\n", in.ID, len(text))
	return header + FormatResult(text, t.aim()-len(header)), nil
}
