package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Message is one chat entry. Reasoning goes back to the server: Qwen-style templates show a model its
// own thinking for the task in hand, and without it every tool round starts its reasoning over.
type Message struct {
	Role        string       `json:"role"`
	Content     string       `json:"content"`
	ToolCalls   []ToolCall   `json:"tool_calls,omitempty"`
	ToolCallID  string       `json:"tool_call_id,omitempty"` // set on role:"tool" replies
	Name        string       `json:"name,omitempty"`         // tool name, echoed for role:"tool"
	CallID      string       `json:"call_id,omitempty"`      // ykode's own id for this result: r1, r2, ...
	Reasoning   string       `json:"reasoning_content,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"` // non-text parts, sent beside the text
}

// ModalityImage is the only non-text input this client knows how to write on the wire. The server's
// own list of modalities decides what reaches a message at all, so a new one is one case here.
const ModalityImage = "image"

// Attachment is a non-text part of a message. Data is base64 without the data URL around it, so the
// session file keeps it as one field and only the request writes the URL a chat API asks for.
type Attachment struct {
	Modality string `json:"modality"` // ModalityImage, or whatever else the server said it takes
	MIME     string `json:"mime"`     // what the server is told the bytes are, e.g. "image/png"
	Data     string `json:"data"`
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
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	// Modalities is what this request's model is known to take as input. It is never sent: it decides
	// which attachments go out at all. The bytes stay in the conversation, so a model that takes them
	// gets them back when the run switches to it.
	Modalities         []string       `json:"-"`
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
	CtxSize int  // 0 when the server does not report it
	Vision  bool // it takes images as input, so an image part of a message means something to it
}

// modelsResponse is the GET /models body.
type modelsResponse struct {
	Data []struct {
		ID   string `json:"id"`
		Meta struct {
			CtxSize       int      `json:"n_ctx"`
			InputModality []string `json:"input_modalities"` // some servers put the modalities here
		} `json:"meta"`
		Architecture struct {
			InputModality []string `json:"input_modalities"` // others, Strata among them, follow the OpenRouter shape
		} `json:"architecture"`
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

// hasFlag says a llama.cpp server was started with this switch.
func hasFlag(args []string, name string) bool { return slices.Contains(args, name) }

// takesImages says this list of input modalities includes images.
func takesImages(modes []string) bool { return slices.Contains(modes, ModalityImage) }

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

// wireMessage is one message as the chat API takes it. Content is the string it has always been, or a
// list of parts once the message carries an attachment: the shape is what the chat template reads.
type wireMessage struct {
	Role       string     `json:"role"`
	Content    any        `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
	Reasoning  string     `json:"reasoning_content,omitempty"`
}

type wirePart struct {
	Type     string   `json:"type"`
	Text     string   `json:"text,omitempty"`
	ImageURL *wireURL `json:"image_url,omitempty"`
}

type wireURL struct {
	URL string `json:"url"`
}

const (
	partText  = "text"
	partImage = "image_url"

	// imagesNote heads the user message that carries what a run of tool results read: the chat API has
	// no room for an attachment inside a tool reply, so it follows the run instead.
	imagesNote = "Images from the tool results above:"
)

// MarshalJSON writes the conversation as the chat API takes it: r1, r2... are ykode's own ids for a
// tool result, kept in the session file so a resume puts it back in the same place, and a chat API
// that has never heard of them should not be sent them.
func (r ChatRequest) MarshalJSON() ([]byte, error) {
	type request ChatRequest // a different type: marshalling a ChatRequest here would recurse
	return json.Marshal(struct {
		request
		Messages []wireMessage `json:"messages"` // shallower than the embedded one, so it is what goes out
	}{request: request(r), Messages: offTheWire(r)})
}

// offTheWire is the conversation as the server should see it: text as it was said, and the attachments
// it can take as parts beside it.
func offTheWire(req ChatRequest) []wireMessage {
	var out []wireMessage
	for i := 0; i < len(req.Messages); i++ {
		m := req.Messages[i]
		if m.Role != "tool" {
			out = append(out, wireOf(m, partsOf(m, req.Modalities)))
			continue
		}
		// Tool replies answer one assistant message and have to stay that run's answers, so what they
		// read travels after the whole run, once, rather than inside each of them.
		end, read := i, []wirePart{}
		for end < len(req.Messages) && req.Messages[end].Role == "tool" {
			read = append(read, attachmentsOf(req.Messages[end].Attachments, req.Modalities)...)
			end++
		}
		for j := i; j < end; j++ {
			out = append(out, wireOf(req.Messages[j], req.Messages[j].Content))
		}
		if len(read) > 0 {
			out = append(out, wireMessage{Role: "user", Content: append([]wirePart{{Type: partText, Text: imagesNote}}, read...)})
		}
		i = end - 1
	}
	return out
}

// wireOf is one message with its content already decided and ykode's own result id left out.
func wireOf(m Message, content any) wireMessage {
	return wireMessage{Role: m.Role, Content: content, ToolCalls: m.ToolCalls,
		ToolCallID: m.ToolCallID, Name: m.Name, Reasoning: m.Reasoning}
}

// partsOf is a message's content: the plain string whenever nothing but text goes with it, since that
// is the shape every server has always been sent, and the parts list only when an attachment is in it.
func partsOf(m Message, modes []string) any {
	read := attachmentsOf(m.Attachments, modes)
	if len(read) == 0 {
		return m.Content
	}
	if m.Content != "" {
		read = append([]wirePart{{Type: partText, Text: m.Content}}, read...)
	}
	return read
}

// attachmentsOf is the part of a message's attachments this request's model can take. An attachment
// that cannot travel is left here, not deleted from the conversation: another model may take it.
func attachmentsOf(atts []Attachment, modes []string) []wirePart {
	var out []wirePart
	for _, a := range atts {
		if a.Modality != ModalityImage || !slices.Contains(modes, a.Modality) {
			continue
		}
		out = append(out, wirePart{Type: partImage, ImageURL: &wireURL{URL: "data:" + a.MIME + ";base64," + a.Data}})
	}
	return out
}
