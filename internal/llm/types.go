package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Message is one chat entry; Reasoning is kept separate and never sent back, which would double the prompt.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"` // set on role:"tool" replies
	Name       string     `json:"name,omitempty"`         // tool name, echoed for role:"tool"
	Reasoning  string     `json:"-"`
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
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
	Stream             bool           `json:"stream"`
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
}

// PromptTokens is the full prompt size, including whatever the server cached.
func (t Timings) PromptTokens() int { return t.CacheN + t.PromptN }

// Turn is a complete model reply, reassembled from the stream.
type Turn struct {
	Message      Message
	FinishReason string // "stop", "tool_calls" or "length"
	Timings      Timings
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
	Error   *apiError `json:"error"`
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

// preview keeps a diagnostic short and on one line.
func preview(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 120 {
		return s[:120] + "..."
	}
	return s
}
