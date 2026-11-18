package tui

import (
	"context"
	"strings"
	"time"

	"github.com/yuriy-kovalchuk/ykode/internal/llm"
)

// probeFunc lists a server's models; the screen takes it as a value so a test can answer instead.
type probeFunc func(ctx context.Context, baseURL string) (string, []llm.ModelInfo, error)

const setupProbeTimeout = 10 * time.Second

// probeServer retries with /v1: llama-server serves the OpenAI routes there, and it is easy to leave off.
func probeServer(ctx context.Context, baseURL string) (string, []llm.ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, setupProbeTimeout)
	defer cancel()
	models, err := llm.NewClient(baseURL).Models(ctx)
	if err == nil {
		return baseURL, models, nil
	}
	if !strings.HasSuffix(baseURL, "/v1") {
		withV1 := baseURL + "/v1"
		if models, err2 := llm.NewClient(withV1).Models(ctx); err2 == nil {
			return withV1, models, nil
		}
	}
	return baseURL, nil, err
}

// probeDoneMsg is what the probe answers the screen with.
type probeDoneMsg struct {
	baseURL string
	models  []llm.ModelInfo
	err     error
}
