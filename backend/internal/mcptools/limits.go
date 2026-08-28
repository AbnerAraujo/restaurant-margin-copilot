package mcptools

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// DefaultToolTimeout bounds every MCP tool call this package exposes
// (Constitution Principle III: "Every MCP tool call MUST carry a
// timeout"). contracts/mcp-tools.md states 5s explicitly for
// get_daily_summary and is silent on the other tools; this package applies
// the same 5s bound uniformly, since none of them do anything get_daily_summary
// doesn't already do (a handful of indexed Postgres reads).
const DefaultToolTimeout = 5 * time.Second

// DefaultMaxToolCallsPerInteraction is the per-interaction tool-call cap
// spec FR-010 and Constitution Principle III require, and what spec
// Acceptance Scenario US3.3 exercises ("a question that would require more
// tool calls than the configured cap... the system stops and reports that
// it could not complete the request, rather than returning a partial,
// unlabeled answer"). It bounds one question-answering interaction's whole
// tool-calling loop (internal/explain), not this server's lifetime.
const DefaultMaxToolCallsPerInteraction = 8

// CallBudget counts tool calls made so far within a single interaction. A
// fresh CallBudget must be created per interaction — internal/explain does
// this once per question — and installed into that interaction's context
// via WithCallBudget, so every tool call the model makes during that one
// interaction (however many turns the tool-calling loop takes) shares the
// same counter. Safe for concurrent use, though in this codebase one
// interaction's tool calls are always made sequentially by the loop that
// owns the budget.
type CallBudget struct {
	max int

	mu   sync.Mutex
	used int
}

// NewCallBudget constructs a CallBudget allowing at most max tool calls.
func NewCallBudget(max int) *CallBudget {
	return &CallBudget{max: max}
}

// Used reports how many calls have been consumed so far.
func (b *CallBudget) Used() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}

// Max reports the configured cap.
func (b *CallBudget) Max() int { return b.max }

// take consumes one call from the budget. It returns false, without
// consuming anything, once the cap has already been reached.
func (b *CallBudget) take() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used >= b.max {
		return false
	}
	b.used++
	return true
}

type callBudgetKey struct{}

// WithCallBudget returns a context carrying budget, so every MCP tool call
// dispatched against that context (through the middleware this package
// installs in RegisterMCPServer) shares one interaction-scoped counter. A
// tool call made against a context with no installed budget (e.g. a direct
// unit-test call to one of this package's core functions, which never goes
// through the MCP dispatch path at all) is unaffected by the cap — only
// timeout-bounded.
func WithCallBudget(ctx context.Context, budget *CallBudget) context.Context {
	return context.WithValue(ctx, callBudgetKey{}, budget)
}

func callBudgetFromContext(ctx context.Context) (*CallBudget, bool) {
	b, ok := ctx.Value(callBudgetKey{}).(*CallBudget)
	return b, ok
}

// ErrToolCallCapExceeded, ErrToolCallTimeout, and ErrToolCallCanceled are
// the typed-error "error" field values timeoutAndBudgetMiddleware can
// produce, exported so internal/explain (and tests) can recognize them
// without string-matching. ErrToolCallTimeout and ErrToolCallCanceled are
// deliberately distinct values: a genuine 5s deadline expiry is a real
// timeout the model can be told about, but a canceled parent context (the
// browser closed, the HTTP request was aborted upstream) is not a timeout
// at all — reporting it as one would be a false claim about what happened,
// which this codebase's refuse-rather-than-guess principle forbids just as
// much for an internal status report as for a margin figure.
const (
	ErrToolCallCapExceeded = "tool_call_cap_exceeded"
	ErrToolCallTimeout     = "tool_call_timeout"
	ErrToolCallCanceled    = "tool_call_canceled"
)

// timeoutAndBudgetMiddleware enforces both of Constitution Principle III's
// requirements at the single point every tool call in this server actually
// passes through — mcp-go's own dispatch path (registered via s.Use in
// RegisterMCPServer, applied to every AddTool'd handler, not something a
// caller could accidentally bypass by holding a *ServerTool directly):
//
//   - a bounded timeout on every call (context.WithTimeout); and
//   - a hard per-interaction call cap, via whatever CallBudget (if any)
//     WithCallBudget installed into ctx upstream.
//
// All three failure modes return a typed, non-protocol-level error result —
// mcp-go's own documented convention ("errors that originate from the tool
// SHOULD be reported inside the result object... so the LLM can see it and
// self-correct") — rather than crashing the interaction or silently
// truncating it. The two ways cctx can end are NOT reported the same way:
// a deadline expiry is a real timeout, but a canceled parent context (the
// browser closed, the HTTP request was aborted upstream) is genuine
// cancellation, not a timeout — see ErrToolCallCanceled's doc comment for
// why conflating the two would itself be a refuse-rather-than-guess
// violation.
func timeoutAndBudgetMiddleware(timeout time.Duration) server.ToolHandlerMiddleware {
	return func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			if budget, ok := callBudgetFromContext(ctx); ok {
				if !budget.take() {
					return errorResult(ToolError{
						Error:  ErrToolCallCapExceeded,
						Reason: fmt.Sprintf("this interaction already made the maximum of %d tool calls", budget.Max()),
					})
				}
			}

			cctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			result, err := next(cctx, req)
			switch {
			case errors.Is(cctx.Err(), context.DeadlineExceeded):
				// The 5s bound was actually hit; whatever next() returned
				// (a partial result, a context-cancellation error, or
				// nothing yet) is not something to hand back as if it
				// completed normally.
				return errorResult(ToolError{
					Error:  ErrToolCallTimeout,
					Reason: fmt.Sprintf("%s exceeded its %s timeout", req.Params.Name, timeout),
				})
			case errors.Is(cctx.Err(), context.Canceled):
				// The PARENT context was canceled (e.g. the client
				// disconnected) — cctx inherits that cancellation from
				// context.WithTimeout, but the 5s bound itself was never
				// reached. Reporting this as "exceeded its timeout" would
				// be a false claim about what happened, so it gets its own
				// distinct, accurate error value instead.
				return errorResult(ToolError{
					Error:  ErrToolCallCanceled,
					Reason: fmt.Sprintf("%s was canceled before it completed — the client disconnected or the request was aborted upstream", req.Params.Name),
				})
			}
			return result, err
		}
	}
}
