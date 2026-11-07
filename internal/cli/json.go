package cli

import (
	"encoding/json"
	"io"
	"sync"

	"github.com/yuriy-kovalchuk/ykode/internal/agent"
)

// JSONWriter is a Sink that prints one JSON object per event, for scripts and the eval harness.
type JSONWriter struct {
	mu  sync.Mutex // read-only tools can run at once, and one line each must stay one line
	enc *json.Encoder
}

func NewJSONWriter(w io.Writer) *JSONWriter { return &JSONWriter{enc: json.NewEncoder(w)} }

// jsonEvent is one line of the stream; empty fields are left out to keep it small.
type jsonEvent struct {
	Kind   string       `json:"kind"`
	Turn   int          `json:"turn,omitempty"`
	Tool   string       `json:"tool,omitempty"`
	Args   string       `json:"args,omitempty"`
	Text   string       `json:"text,omitempty"`
	Target []string     `json:"target,omitempty"`
	Stats  *agent.Stats `json:"stats,omitempty"`
}

// Emit writes the event; a script has nobody to answer a Confirm, so it refuses.
func (j *JSONWriter) Emit(ev agent.Event) agent.Decision {
	j.mu.Lock()
	defer j.mu.Unlock()
	rec := jsonEvent{Kind: ev.Kind.String(), Turn: ev.Turn, Tool: ev.Tool, Args: ev.Args, Text: ev.Text, Target: ev.Target}
	if ev.Stats != (agent.Stats{}) {
		rec.Stats = &ev.Stats
	}
	_ = j.enc.Encode(rec) // a closed stdout ends the run; the agent reports the rest
	return agent.Refuse
}

// usageRecord closes the stream with what the run cost.
type usageRecord struct {
	Kind         string  `json:"kind"`
	Turns        int     `json:"turns"`
	LastPrompt   int     `json:"last_prompt"`
	LastCached   int     `json:"last_cached"`
	Generated    int     `json:"generated"`
	AllPrompts   int     `json:"all_prompts"`
	NextEstimate int     `json:"next_estimate"`
	Context      int     `json:"context"`
	BytesPerTok  float64 `json:"bytes_per_token"`
}

func (j *JSONWriter) Usage(u agent.Usage, ctxSize int) {
	_ = j.enc.Encode(usageRecord{
		Kind: "usage", Turns: u.Turns, LastPrompt: u.PromptTok, LastCached: u.CachedTok,
		Generated: u.GenTok, AllPrompts: u.TotalPrompt, NextEstimate: u.EstTok,
		Context: ctxSize, BytesPerTok: u.BytesPerTok,
	})
}

// Failure reports why the run stopped.
func (j *JSONWriter) Failure(err error) {
	_ = j.enc.Encode(jsonEvent{Kind: "error", Text: err.Error()})
}
