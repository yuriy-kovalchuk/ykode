package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Message is one chat entry. Reasoning goes back to the server: Qwen-style templates show a model its
// own thinking for the task in hand, and without it every tool round starts its reasoning over.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"` // set on role:"tool" replies
	Name       string     `json:"name,omitempty"`         // tool name, echoed for role:"tool"
	CallID     string     `json:"call_id,omitempty"`      // ykode's own id for this result: r1, r2, ...
	Reasoning  string     `json:"reasoning_content,omitempty"`
}

// ToolCall is a function call the model asked for.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ArgsJSON normalises arguments to object bytes: the wire format nests a string, some servers send an object.
func (f FunctionCall) ArgsJSON() (json.RawMessage, error) {
	raw := bytes.TrimSpace(f.Arguments)
	if len(raw) == 0 {
		return json.RawMessage("{}"), nil
	}
	if raw[0] == '"' { // wrapped in a string: unwrap once
		var nested string
		if err := json.Unmarshal(raw, &nested); err != nil {
			return nil, fmt.Errorf("arguments string: %w", err)
		}
		raw = bytes.TrimSpace([]byte(nested))
		if len(raw) == 0 {
			return json.RawMessage("{}"), nil
		}
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("arguments are not valid JSON: %s", preview(string(raw)))
	}
	return raw, nil
}

// ChatRequest is the POST /chat/completions body. It is always streamed.
type ChatRequest struct {
	Model              string         `json:"model"`
	Messages           []Message      `json:"messages"`
	Tools              []ToolSpec     `json:"tools,omitempty"`
	ToolChoice         string         `json:"tool_choice,omitempty"`
	MaxTokens          int            `json:"max_tokens,omitempty"`
	ThinkingBudget     int            `json:"thinking_budget_tokens,omitempty"` // llama.cpp: caps the thinking phase inside MaxTokens
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
	TimingsPerToken    bool           `json:"timings_per_token,omitempty"` // llama.cpp: speeds in every chunk, not only the last
	Stream             bool           `json:"stream"`
	StreamOptions      *StreamOptions `json:"stream_options,omitempty"`
	Sampling
}

// StreamOptions asks an OpenAI-style server to count the request in the stream's last chunk. llama.cpp
// says as much in its timings; the others say it only when asked, and without it the screen has no
// prompt size, no cache figure and nothing to learn the byte ratio from.
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// Sampling is what a model card recommends; a nil field leaves the server's default.
type Sampling struct {
	Temperature     *float64 `json:"temperature,omitempty" yaml:"temperature,omitempty"`
	TopP            *float64 `json:"top_p,omitempty" yaml:"top_p,omitempty"`
	TopK            *int     `json:"top_k,omitempty" yaml:"top_k,omitempty"`
	MinP            *float64 `json:"min_p,omitempty" yaml:"min_p,omitempty"`
	PresencePenalty *float64 `json:"presence_penalty,omitempty" yaml:"presence_penalty,omitempty"`
}

// ModelCard is the sampling written down for one model, with thinking on and off. It travels to the run
// loop so a model switch picks up the new model's numbers instead of carrying the old model's.
type ModelCard struct {
	Thinking Sampling
	NoThink  Sampling
}

// ToolSpec advertises one tool to the model.
type ToolSpec struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Delta is one streamed piece, thinking and answer kept apart so they can render differently.
type Delta struct {
	Reasoning string
	Content   string
	Call      *CallDelta // a piece of a tool call, so a screen can show a long one being written
	Timings   *Timings   // the server's speeds so far, on servers that send them while streaming
}

// CallDelta is one fragment of a tool call's arguments; Name is known from the first fragment on.
type CallDelta struct {
	Index int
	Name  string
	Args  string
}

// Timings is llama.cpp's per-turn accounting in the last chunk; other servers leave it zero.
type Timings struct {
	CacheN             int     `json:"cache_n"`
	PromptN            int     `json:"prompt_n"`
	PredictedN         int     `json:"predicted_n"`
	PromptMS           float64 `json:"prompt_ms"`
	PredictedMS        float64 `json:"predicted_ms"`
	PromptPerSecond    float64 `json:"prompt_per_second"`
	PredictedPerSecond float64 `json:"predicted_per_second"`
	DraftN             int     `json:"draft_n"`          // tokens a draft model proposed
	DraftNAccepted     int     `json:"draft_n_accepted"` // of those, what the big model kept
}

// PromptTokens is the full prompt size, including whatever the server cached.
func (t Timings) PromptTokens() int { return t.CacheN + t.PromptN }

// Turn is a complete model reply, reassembled from the stream.
type Turn struct {
	Message      Message
	FinishReason string // "stop", "tool_calls" or "length"
	Timings      Timings
	Unfinished   bool // the stream ended with no [DONE] and no finish reason: the reply may be cut short
}

// ModelInfo is one entry from GET /models.
type ModelInfo struct {
	ID      string
	CtxSize int // 0 when the server does not report it
}

// modelsResponse is the GET /models body.
type modelsResponse struct {
	Data []struct {
		ID   string `json:"id"`
		Meta struct {
			CtxSize int `json:"n_ctx"`
		} `json:"meta"`
		Status struct {
			Args []string `json:"args"` // llama.cpp: the server's own command line
		} `json:"status"`
	} `json:"data"`
}

// flagValue reads the value after --name in a llama.cpp argument list.
func flagValue(args []string, name string) (int, bool) {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			var n int
			if _, err := fmt.Sscanf(args[i+1], "%d", &n); err == nil && n > 0 {
				return n, true
			}
		}
	}
	return 0, false
}

// chatChunk is one SSE data payload.
type chatChunk struct {
	Choices []struct {
		Delta struct {
			Role             string             `json:"role"`
			Content          string             `json:"content"`
			ReasoningContent string             `json:"reasoning_content"`
			ToolCalls        []toolCallFragment `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Timings *Timings  `json:"timings"`
	Usage   *usage    `json:"usage"`
	Error   *apiError `json:"error"`
}

// usage is the OpenAI-style count of a request, sent in the last chunk of a stream.
type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	Details          struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

// toolCallFragment is a piece of a call: only the first carries id and name, keyed by index.
type toolCallFragment struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type apiError struct {
	Message string `json:"message"`
	Code    any    `json:"code"`
}

func (e apiError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("code %v", e.Code)
}

// UnmarshalJSON takes an error written either as an object or as a bare string. OpenAI-compatible
// servers are not consistent about this, and refusing the string form turns a server saying
// "model is loading" into a JSON decode error the user cannot act on - and hides the one phrase the
// retry path knows how to explain.
func (e *apiError) UnmarshalJSON(raw []byte) error {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		e.Message = text
		return nil
	}
	type asObject apiError // a different type: unmarshalling into apiError here would recurse
	return json.Unmarshal(raw, (*asObject)(e))
}

// preview keeps a diagnostic short and on one line.
func preview(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 120 {
		return s[:120] + "..."
	}
	return s
}

// offTheWire is a conversation as the server should see it: r1, r2... are ykode's own ids for a
// tool result, kept in the session file so a resume puts it back in the same place, and a chat
// API that has never heard of them should not be sent them.
func offTheWire(messages []Message) []Message {
	out := make([]Message, len(messages))
	for i, m := range messages {
		m.CallID = ""
		out[i] = m
	}
	return out
}
