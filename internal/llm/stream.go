package llm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

var (
	sseDataPrefix = []byte("data:")
	sseDone       = []byte("[DONE]")
)

// readSSE walks the event stream and feeds every chunk to the accumulator.
// It stops at the [DONE] sentinel or at the end of the body, whichever comes
// first: some servers just close the connection.
func readSSE(body io.Reader, acc *streamAccumulator, onDelta func(Delta)) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytes.HasPrefix(line, sseDataPrefix) {
			continue // blank separators, "event:" and comment lines
		}
		data := bytes.TrimSpace(line[len(sseDataPrefix):])
		if bytes.Equal(data, sseDone) {
			break
		}
		var chunk chatChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			return fmt.Errorf("decode stream chunk: %w", err)
		}
		if chunk.Error != nil {
			return fmt.Errorf("server error: %w", *chunk.Error)
		}
		acc.absorb(chunk, onDelta)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stream: %w", err)
	}
	return nil
}

// streamAccumulator holds the whole reply while it arrives: the answer, the
// thinking, the finish reason, llama.cpp timings, and tool calls keyed by index.
type streamAccumulator struct {
	content   strings.Builder
	reasoning strings.Builder
	finish    string
	timings   Timings
	calls     map[int]*callAccumulator
	order     []int
}

func newStreamAccumulator() *streamAccumulator {
	return &streamAccumulator{calls: map[int]*callAccumulator{}}
}

func (a *streamAccumulator) absorb(chunk chatChunk, onDelta func(Delta)) {
	if chunk.Timings != nil {
		a.timings = *chunk.Timings
	}
	for _, choice := range chunk.Choices {
		if choice.FinishReason != "" {
			a.finish = choice.FinishReason
		}
		d := choice.Delta
		if d.ReasoningContent != "" {
			a.reasoning.WriteString(d.ReasoningContent)
			onDelta(Delta{Reasoning: d.ReasoningContent})
		}
		if d.Content != "" {
			a.content.WriteString(d.Content)
			onDelta(Delta{Content: d.Content})
		}
		for _, frag := range d.ToolCalls {
			acc, seen := a.calls[frag.Index]
			if !seen {
				acc = &callAccumulator{}
				a.calls[frag.Index] = acc
				a.order = append(a.order, frag.Index)
			}
			acc.add(frag)
		}
	}
}

// turn builds the finished reply. Tool calls come back in the order their first
// fragment arrived, which is the order the model asked for them.
func (a *streamAccumulator) turn() Turn {
	turn := Turn{
		Message: Message{
			Role:      "assistant",
			Content:   a.content.String(),
			Reasoning: a.reasoning.String(),
		},
		FinishReason: a.finish,
		Timings:      a.timings,
	}
	for _, index := range a.order {
		turn.Message.ToolCalls = append(turn.Message.ToolCalls, a.calls[index].call())
	}
	return turn
}

// callAccumulator stitches tool call fragments back into one call. Only the
// first fragment of a call carries the id and the name.
type callAccumulator struct {
	id   string
	name string
	args strings.Builder
}

func (a *callAccumulator) add(frag toolCallFragment) {
	if frag.ID != "" {
		a.id = frag.ID
	}
	if frag.Function.Name != "" {
		a.name = frag.Function.Name
	}
	a.args.WriteString(frag.Function.Arguments)
}

func (a *callAccumulator) call() ToolCall {
	return ToolCall{
		ID:   a.id,
		Type: "function",
		Function: FunctionCall{
			Name:      a.name,
			Arguments: json.RawMessage(a.args.String()),
		},
	}
}
