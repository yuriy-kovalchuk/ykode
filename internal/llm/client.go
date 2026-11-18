// Package llm talks to an OpenAI-compatible chat-completions server over plain net/http.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// streamIdleTimeout bounds the gap between events: only a stream that stops producing at all counts as dead
	streamIdleTimeout = 2 * time.Minute
	// firstByteWait bounds the wait for a server to start answering, which a rewritten prompt makes longer
	firstByteWait = 10 * time.Minute

	maxResponseBytes  = 8 << 20 // refuse a runaway server
	maxErrorBodyBytes = 512     // how much of an error body to quote
	maxLineBytes      = 1 << 20 // one SSE line can hold a whole message
)

// Client is a stateless caller of one server.
type Client struct {
	baseURL string
	http    *http.Client
	Idle    time.Duration // longest silence a stream may keep before it counts as dead
	opening time.Duration // how long the server may take to send its first byte

	// Session names this conversation on every request, so a proxy can keep it on its prompt-cache slot
	Session string
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		// no whole-request timeout: a long generation is fine, silence is not
		http:    &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()},
		Idle:    streamIdleTimeout,
		opening: firstByteWait,
	}
}

func (c *Client) newRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	if c.Session != "" {
		req.Header.Set("X-Session-Id", c.Session)
	}
	return req, nil
}

// idleWatchdog cancels a stream that goes quiet, without capping how long it may take overall.
type idleWatchdog struct {
	ctx    context.Context
	cancel context.CancelFunc
	timer  *time.Timer
	idle   time.Duration
	// started is written only by the reading goroutine and read after it returns, so it needs no lock
	started bool
}

func newIdleWatchdog(ctx context.Context, opening, idle time.Duration) *idleWatchdog {
	w := &idleWatchdog{idle: idle}
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.timer = time.AfterFunc(opening, w.cancel)
	return w
}

func (w *idleWatchdog) stop() { w.timer.Stop(); w.cancel() }

func (w *idleWatchdog) wrap(r io.Reader) io.Reader { return &pokeReader{r: r, w: w} }

type pokeReader struct {
	r io.Reader
	w *idleWatchdog
}

func (p *pokeReader) Read(buf []byte) (int, error) {
	n, err := p.r.Read(buf)
	if n > 0 {
		p.w.started = true
		p.w.timer.Reset(p.w.idle)
	}
	return n, err
}

// Models lists the served models and their context size, which llama.cpp reports in meta.n_ctx or status.args.
func (c *Client) Models(ctx context.Context) ([]ModelInfo, error) {
	req, err := c.newRequest(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
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
		// a server that thought about what it feeds the model says so; llama.cpp says only that it has a projector
		info.Vision = takesImages(m.Meta.InputModality) || takesImages(m.Architecture.InputModality) ||
			hasFlag(m.Status.Args, "--mmproj")
		models = append(models, info)
	}
	if len(models) == 0 {
		return nil, errors.New("server answered /models with no models")
	}
	return models, nil
}

// ChatStream streams a turn; onDelta sees each reasoning and answer fragment as it arrives.
func (c *Client) ChatStream(ctx context.Context, body ChatRequest, onDelta func(Delta)) (Turn, error) {
	var empty Turn
	body.Stream = true
	body.StreamOptions = &StreamOptions{IncludeUsage: true}
	payload, err := json.Marshal(body) // MarshalJSON is what puts the conversation in the shape the API takes
	if err != nil {
		return empty, fmt.Errorf("encode request: %w", err)
	}
	watchdog := newIdleWatchdog(ctx, c.opening, c.Idle)
	defer watchdog.stop()

	resp, err := c.postChat(watchdog.ctx, payload)
	if err != nil {
		return empty, err
	}
	defer func() { _ = resp.Body.Close() }()

	acc := newStreamAccumulator()
	if err := readSSE(watchdog.wrap(resp.Body), acc, onDelta); err != nil {
		if ctx.Err() == nil && watchdog.ctx.Err() != nil {
			wait := c.opening
			if watchdog.started {
				wait = c.Idle
			}
			return empty, fmt.Errorf("server sent nothing for %s: the model may still be loading, or the connection died", wait)
		}
		return empty, err
	}
	return acc.turn(), nil
}

// chatAttempts is how often one turn is sent: once, plus once more for a failure that could pass later.
const chatAttempts = 2

// retryWait separates a retry from the failure that caused it.
const retryWait = 500 * time.Millisecond

// postChat rejects any response that is not an event stream, so the rest handles one shape.
func (c *Client) postChat(ctx context.Context, payload []byte) (*http.Response, error) {
	var firstErr error
	for attempt := 1; ; attempt++ {
		resp, err := c.sendChat(ctx, payload)
		if err == nil {
			return resp, nil
		}
		if attempt == chatAttempts || !couldPassLater(err) {
			if attempt > 1 && firstErr != nil {
				// the first failure usually explains itself, where the second reads as a bare timeout
				return nil, fmt.Errorf("%w (after a first failure: %v)", err, firstErr)
			}
			return nil, err
		}
		firstErr = err
		select {
		case <-time.After(retryWait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (c *Client) sendChat(ctx context.Context, payload []byte) (*http.Response, error) {
	req, err := c.newRequest(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
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
		defer func() { _ = resp.Body.Close() }()
		return nil, statusError(resp)
	}
	// a proxy may answer a streaming request with one JSON body, which as SSE looks like an empty reply
	if ct := resp.Header.Get("Content-Type"); strings.HasPrefix(ct, "application/json") {
		defer func() { _ = resp.Body.Close() }()
		return nil, fmt.Errorf("server answered %s instead of an event stream: %s", ct, preview(quotedBody(resp)))
	}
	return resp, nil
}

// chatFailure is an HTTP status plus the body the server explained it with.
type chatFailure struct {
	status string
	code   int
	detail string
}

func (f chatFailure) Error() string {
	msg := "HTTP " + f.status
	if f.detail != "" {
		msg += ": " + f.detail
	}
	if modelIsLoading(f.detail) {
		msg += " (a router like this one loads models on demand, so the model may still be starting)"
	}
	return msg
}

// overflowWords is how servers say a request was bigger than the model's window.
var overflowWords = regexp.MustCompile(`(?i)(exceeds? the (available )?context|context (size|length|window)|maximum context|too many tokens|prompt is too long|n_ctx)`)

// IsOverflow says a request did not fit the window, so the run can make room and ask again
func IsOverflow(err error) bool {
	var failure chatFailure
	if !errors.As(err, &failure) {
		return false
	}
	return (failure.code == http.StatusBadRequest || failure.code == http.StatusRequestEntityTooLarge) && overflowWords.MatchString(failure.detail)
}

// statedTokens is where a server names the size of the request it refused, next to the size it has
var statedTokens = regexp.MustCompile(`(?i)(?:resulted in|messages? (?:are|total)|prompt(?: of| is)?|total(?: of| is)?|used|with)\s+(\d{2,9})\s+tokens`)

// StatedPromptTokens is the size a server said the overflowing request was, or 0 when it did not say
func StatedPromptTokens(err error) int {
	var failure chatFailure
	if !errors.As(err, &failure) {
		return 0
	}
	match := statedTokens.FindStringSubmatch(failure.detail)
	if len(match) < 2 {
		return 0
	}
	n, err := strconv.Atoi(match[1])
	if err != nil || n <= 0 || n > 100_000_000 {
		return 0 // a number this big is not a token count
	}
	return n
}

// couldPassLater reports a failure where asking again is worth the wait.
func couldPassLater(err error) bool {
	var failure chatFailure
	if !errors.As(err, &failure) {
		// a refused or dropped connection: the server may be restarting or swapping a model out
		return true
	}
	switch failure.code {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, http.StatusTooManyRequests:
		return true
	}
	return modelIsLoading(failure.detail)
}

// loadingWords is how servers say a model is not in memory yet.
var loadingWords = regexp.MustCompile(`(?i)(model is loading|loading model|not loaded|no model|server is (busy|starting)|overloaded|temporarily unavailable|no healthy upstream)`)

func modelIsLoading(detail string) bool { return loadingWords.MatchString(detail) }

// statusError quotes the beginning of an error response body.
func statusError(resp *http.Response) error {
	return chatFailure{status: resp.Status, code: resp.StatusCode, detail: quotedBody(resp)}
}

// quotedBody reads the beginning of a response body that is about to be dropped.
func quotedBody(resp *http.Response) string {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	return strings.TrimSpace(string(body))
}
