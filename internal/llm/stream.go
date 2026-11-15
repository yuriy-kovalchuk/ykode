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

// readSSE feeds every chunk to the accumulator and stops at [DONE] or the end of the body; onDelta may be nil.
func readSSE(body io.Reader, acc *streamAccumulator, onDelta func(Delta)) error {
	if onDelta == nil {
		onDelta = func(Delta) {}
	}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	done := false
	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytes.HasPrefix(line, sseDataPrefix) {
			continue // blank separators, "event:" and comment lines
		}
		data := bytes.TrimSpace(line[len(sseDataPrefix):])
		if bytes.Equal(data, sseDone) {
			done = true
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
	acc.done = done
	return nil
}

// streamAccumulator collects answer, thinking, finish reason, timings and tool calls in arrival order.
type streamAccumulator struct {
	content   strings.Builder
	reasoning strings.Builder
	finish    string
	done      bool // the server sent [DONE]
	timings   Timings
	calls     map[int]*callAccumulator
	order     []int // the indexes in the order their first fragment arrived
}

func newStreamAccumulator() *streamAccumulator {
	return &streamAccumulator{calls: map[int]*callAccumulator{}}
}

func (a *streamAccumulator) absorb(chunk chatChunk, onDelta func(Delta)) {
	if chunk.Timings != nil {
		// A timings chunk carries the speeds, and usually the counts too - but not always, and not in the
		// order a server chooses. Assigning it whole would throw away counts a usage block had already
		// brought, which is what the turn's whole budget is measured from, so the counts are merged.
		a.timings = mergeTimings(a.timings, *chunk.Timings)
		onDelta(Delta{Timings: chunk.Timings})
	}
	if u := chunk.Usage; u != nil {
		a.applyUsage(u)
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
			if frag.Function.Arguments != "" || frag.Function.Name != "" {
				onDelta(Delta{Call: &CallDelta{Index: frag.Index, Name: acc.name, Args: frag.Function.Arguments}})
			}
		}
	}
}

// applyUsage folds an OpenAI-style count into the figures. llama.cpp sends its own timings and no
// usage block; other servers send usage and no timings; some send both, in either order. A field the
// server never sent stays zero rather than being guessed at, and a count already held is kept, so the
// order the two kinds of chunk arrive in cannot lose a number.
func (a *streamAccumulator) applyUsage(u *usage) {
	if a.timings.PromptTokens() == 0 && u.PromptTokens > 0 {
		// the total is what the server counted; the cached share is part of it, and a server that says
		// one without the other leaves the part it did not say at zero
		cached := min(u.Details.CachedTokens, u.PromptTokens)
		a.timings.CacheN, a.timings.PromptN = cached, u.PromptTokens-cached
	}
	if a.timings.PredictedN == 0 && u.CompletionTokens > 0 {
		a.timings.PredictedN = u.CompletionTokens
	}
}

// mergeTimings lays a later timings block over an earlier one, field by field. A zero in the new block
// says that server said nothing about that figure, not that it fell to zero, so the earlier figure
// stands: a server that sends its counts in one chunk and its speeds in another keeps both.
func mergeTimings(earlier, later Timings) Timings {
	if later.CacheN == 0 && later.PromptN == 0 {
		later.CacheN, later.PromptN = earlier.CacheN, earlier.PromptN
	}
	if later.PredictedN == 0 {
		later.PredictedN = earlier.PredictedN
	}
	pairs := [][2]*float64{
		{&later.PromptMS, &earlier.PromptMS},
		{&later.PredictedMS, &earlier.PredictedMS},
		{&later.PromptPerSecond, &earlier.PromptPerSecond},
		{&later.PredictedPerSecond, &earlier.PredictedPerSecond},
	}
	for _, p := range pairs {
		if *p[0] == 0 {
			*p[0] = *p[1]
		}
	}
	if later.DraftN == 0 {
		later.DraftN, later.DraftNAccepted = earlier.DraftN, earlier.DraftNAccepted
	}
	return later
}

// turn builds the reply; tool calls come back in the order their first fragment arrived, which is the
// order the server chose to open them in.
func (a *streamAccumulator) turn() Turn {
	turn := Turn{
		Message: Message{
			Role:      "assistant",
			Content:   a.content.String(),
			Reasoning: a.reasoning.String(),
		},
		FinishReason: a.finish,
		Timings:      a.timings,
		Unfinished:   !a.done && a.finish == "",
	}
	for _, index := range a.order {
		turn.Message.ToolCalls = append(turn.Message.ToolCalls, a.calls[index].call())
	}
	return turn
}

// callAccumulator stitches fragments into one call; only the first carries id and name.
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
