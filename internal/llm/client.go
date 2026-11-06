// Package llm talks to an OpenAI-compatible chat-completions server over plain
// net/http. It knows nothing about tools or the agent loop.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	httpTimeout       = 10 * time.Minute // local inference on a big prompt is slow
	maxResponseBytes  = 8 << 20          // refuse a runaway server
	maxErrorBodyBytes = 512              // how much of an error body to quote
	maxLineBytes      = 1 << 20          // one SSE line can hold a whole message
)

// Client is a stateless caller of one server.
type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		http:    &http.Client{Timeout: httpTimeout},
	}
}

// Models lists the models the server serves, with their context size when the
// server reports one. llama.cpp exposes the ctx both in meta.n_ctx (loaded) and
// in status.args (the launch command of an unloaded model).
func (c *Client) Models(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, statusError(resp)
	}
	var payload modelsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode /models: %w", err)
	}
	var models []ModelInfo
	for _, m := range payload.Data {
		if m.ID == "" {
			continue
		}
		info := ModelInfo{ID: m.ID, CtxSize: m.Meta.CtxSize}
		if info.CtxSize == 0 {
			info.CtxSize, _ = flagValue(m.Status.Args, "--ctx-size")
		}
		models = append(models, info)
	}
	if len(models) == 0 {
		return nil, errors.New("server answered /models with no models")
	}
	return models, nil
}

// ChatStream sends a turn and reassembles the streamed reply. onDelta is called
// for every reasoning and answer fragment as it arrives; it runs on the
// caller's goroutine and must not block for long.
func (c *Client) ChatStream(ctx context.Context, body ChatRequest, onDelta func(Delta)) (Turn, error) {
	var empty Turn
	body.Stream = true
	payload, err := json.Marshal(body)
	if err != nil {
		return empty, fmt.Errorf("encode request: %w", err)
	}
	resp, err := c.postChat(ctx, payload)
	if err != nil {
		return empty, err
	}
	defer resp.Body.Close()

	acc := newStreamAccumulator()
	if err := readSSE(resp.Body, acc, onDelta); err != nil {
		return empty, err
	}
	return acc.turn(), nil
}

// postChat sends the request and rejects anything that is not an event stream,
// so the rest of the path only has to deal with one shape of response.
func (c *Client) postChat(ctx context.Context, payload []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, statusError(resp)
	}
	// A proxy or an errored server can answer a streaming request with one plain
	// JSON body. Read as SSE that would look like an empty model reply instead of
	// a failure.
	if ct := resp.Header.Get("Content-Type"); strings.HasPrefix(ct, "application/json") {
		defer resp.Body.Close()
		return nil, fmt.Errorf("server answered %s instead of an event stream: %s", ct, preview(quotedBody(resp)))
	}
	return resp, nil
}

// statusError quotes the beginning of an error response body.
func statusError(resp *http.Response) error {
	detail := quotedBody(resp)
	if detail == "" {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	return fmt.Errorf("HTTP %s: %s", resp.Status, detail)
}

// quotedBody reads the beginning of a response body that is about to be dropped.
func quotedBody(resp *http.Response) string {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	return strings.TrimSpace(string(body))
}
