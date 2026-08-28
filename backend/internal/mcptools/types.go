// Package mcptools is the ONLY path from the model layer to
// internal/reconcile's persisted output (Constitution Principle III): a
// fixed, typed set of read-only MCP tools (mark3labs/mcp-go), each backed
// by internal/storage, each returning either an already-computed value or a
// typed error object — never a best-guess, never open SQL, never a
// free-form filter. See specs/001-margin-reconciliation-qa/contracts/
// mcp-tools.md for the exact contract this package implements.
//
// Every exported "core" function here (GetDailySummary, GetMarginDelta,
// ListDiscrepancies) is deliberately independent of the MCP protocol
// envelope: it takes plain Go arguments and returns
// (*Result, *ToolError, error). That three-way return is the load-bearing
// design decision in this package:
//
//   - a non-nil *ToolError means the request is well-formed but cannot be
//     fulfilled from the data available (no_data, insufficient_data,
//     invalid_input) — the contract's "typed error object, never a
//     best-guess value" — and is exactly what tests exercise without
//     needing to go through the MCP JSON-RPC envelope at all;
//   - a non-nil error means something actually went wrong underneath
//     (a live Postgres failure) and is not a business outcome the model
//     should be narrating a plausible story around.
//
// server.go / reconciliation_tools.go's MCP tool handlers are a thin
// adapter over these functions: parse CallToolRequest arguments, call the
// core function, and translate the result into an mcp.CallToolResult,
// following mcp-go's own documented convention that an error the tool
// itself produces belongs inside the result (IsError: true) so the model
// can see it and react, not as a protocol-level error.
package mcptools

import (
	"fmt"
	"time"
)

// dateLayout is the YYYY-MM-DD format every tool contract in
// contracts/mcp-tools.md uses for date and period fields.
const dateLayout = "2006-01-02"

// Period is the {start, end} shape every ranged tool input in
// contracts/mcp-tools.md uses (get_margin_delta's period_a/period_b,
// list_discrepancies' period).
type Period struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// parse validates and converts a Period's string fields into time.Time,
// refusing (rather than silently truncating or swapping) a malformed date
// or an end before its start.
func (p Period) parse() (start, end time.Time, err error) {
	start, err = time.Parse(dateLayout, p.Start)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("start %q is not YYYY-MM-DD: %w", p.Start, err)
	}
	end, err = time.Parse(dateLayout, p.End)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("end %q is not YYYY-MM-DD: %w", p.End, err)
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("end %s is before start %s", p.End, p.Start)
	}
	return start, end, nil
}

// ToolError is the typed "cannot fulfill this request" payload every tool
// in this package returns instead of guessing (contracts/mcp-tools.md's
// cross-cutting rule; Constitution Principle II). It is plain data, not a
// Go error — callers get it back as an explicit second/third return value
// (never smuggled inside err) so a caller can never accidentally treat a
// legitimate "no_data" outcome as an unexpected failure, or vice versa.
//
// Error values used by this package: "no_data" (get_daily_summary /
// list_discrepancies: no reconciliation was ever computed for the
// requested date), "insufficient_data" (get_margin_delta: at least one day
// in one of the two periods has no reconciliation), and "invalid_input"
// (a malformed date, an end before a start, or — list_discrepancies — a
// request that supplied both or neither of date/period).
type ToolError struct {
	Error   string   `json:"error"`
	Reason  string   `json:"reason,omitempty"`
	Missing []string `json:"missing,omitempty"`
}

func invalidInput(reason string) *ToolError {
	return &ToolError{Error: "invalid_input", Reason: reason}
}
