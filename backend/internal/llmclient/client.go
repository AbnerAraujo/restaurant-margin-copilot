// Package llmclient is the single shared entry point this project uses to
// talk to the Anthropic API. Both the ambiguity gate (internal/ambiguity/,
// Claude Haiku 4.5) and the explanation step (internal/explain/, Claude
// Sonnet 5) go through this wrapper rather than constructing their own SDK
// clients, so every model call is instrumented, timed, and timed-out the
// same way (Constitution Principle VI, Principle III's timeout requirement).
//
// This package never computes a margin, delta, or ROI figure — it only
// carries a question/prompt to the model and returns the model's text and
// usage. Constitution Principle I's line is drawn in the callers of this
// package (internal/ambiguity, internal/explain), not here, but this
// package deliberately exposes nothing that would let a caller ask the
// model to do arithmetic on raw rows (no query/tool that hands over
// unbounded data — see internal/mcptools for the typed boundary).
package llmclient

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// DefaultTimeout bounds every call made through this client, per
// Constitution Principle III ("Every MCP tool call MUST carry a timeout")
// extended to the model calls that sit alongside those tool calls.
const DefaultTimeout = 30 * time.Second

// Client wraps the Anthropic SDK client with this project's defaults:
// a bounded timeout per call, and a return shape that always carries usage
// (tokens) and latency so callers can hand it straight to
// internal/instrumentation without re-deriving anything.
type Client struct {
	sdk     anthropic.Client
	timeout time.Duration
}

// Opt configures a Client at construction time.
type Opt func(*Client)

// WithTimeout overrides DefaultTimeout for every call made through this
// Client.
func WithTimeout(d time.Duration) Opt {
	return func(c *Client) { c.timeout = d }
}

// WithAPIKey sets the Anthropic API key explicitly, overriding whatever the
// SDK would otherwise resolve from the environment (ANTHROPIC_API_KEY, an
// `ant auth login` profile, etc). Mainly useful for tests; production code
// should rely on the environment so the key never appears in source or
// flags.
func WithAPIKey(key string) Opt {
	return func(c *Client) {
		c.sdk = anthropic.NewClient(option.WithAPIKey(key))
	}
}

// New constructs a Client. With no options, it picks up ANTHROPIC_API_KEY
// (or any other credential source the SDK resolves — see the SDK's own
// auth precedence) from the environment automatically, per
// anthropic.NewClient()'s documented default behavior.
func New(opts ...Opt) *Client {
	c := &Client{
		sdk:     anthropic.NewClient(),
		timeout: DefaultTimeout,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// MessageRequest is the input to CreateMessage. It is intentionally narrow:
// a model, an optional system prompt, a max token budget, the message
// history, and (added for internal/explain's tool-calling loop, tasks.md
// T021) an optional set of tool definitions — nothing that would let a
// caller smuggle raw database rows or open-ended computation through this
// layer (Constitution Principle III; the Tools this project ever passes
// here come from internal/mcptools' fixed, typed set, never a free-form
// query tool).
type MessageRequest struct {
	Model     string
	System    string
	MaxTokens int64
	Messages  []anthropic.MessageParam
	Tools     []anthropic.ToolUnionParam
}

// MessageResult is what every caller needs to both use the model's answer
// and instrument the call: the text, whether the model refused, the
// token/latency figures internal/instrumentation logs verbatim, and (added
// alongside MessageRequest.Tools) the full raw content block list, so a
// tool-calling caller can walk it for anthropic.ToolUseBlock values that
// Text alone discards. Text remains just the concatenated text blocks, so
// existing non-tool-calling callers (internal/ambiguity) are unaffected.
type MessageResult struct {
	Text            string
	ContentBlocks   []anthropic.ContentBlockUnion
	StopReason      anthropic.StopReason
	Refused         bool
	RefusalCategory string
	InputTokens     int64
	OutputTokens    int64
	Latency         time.Duration
}

// CreateMessage sends one request to the Anthropic Messages API, bounded by
// the Client's configured timeout, and normalizes the response into a
// MessageResult. It performs no retries and no tool-calling loop — those
// belong to the specific caller (internal/ambiguity, internal/explain) that
// knows what tools it is allowed to offer and how many iterations it may
// take (Constitution Principle III's per-interaction call cap).
func (c *Client) CreateMessage(ctx context.Context, req MessageRequest) (*MessageResult, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	params := anthropic.MessageNewParams{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Messages:  req.Messages,
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	if len(req.Tools) > 0 {
		params.Tools = req.Tools
	}

	start := time.Now()
	resp, err := c.sdk.Messages.New(ctx, params)
	latency := time.Since(start)
	if err != nil {
		return nil, fmt.Errorf("llmclient: create message: %w", err)
	}

	result := &MessageResult{
		StopReason:    resp.StopReason,
		ContentBlocks: resp.Content,
		InputTokens:   resp.Usage.InputTokens,
		OutputTokens:  resp.Usage.OutputTokens,
		Latency:       latency,
	}

	if resp.StopReason == anthropic.StopReasonRefusal {
		result.Refused = true
		result.RefusalCategory = string(resp.StopDetails.Category)
		return result, nil
	}

	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok {
			result.Text += tb.Text
		}
	}
	return result, nil
}

// EstimatedCostUSD is a convenience wrapper around EstimateCostUSD for a
// MessageResult produced by this client, so callers don't have to thread
// the model string through separately.
func (r *MessageResult) EstimatedCostUSD(model string) (float64, error) {
	if r == nil {
		return 0, errors.New("llmclient: nil result")
	}
	return EstimateCostUSD(model, r.InputTokens, r.OutputTokens)
}
