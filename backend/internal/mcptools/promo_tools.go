// This file covers get_promotion_roi and list_negative_roi_promotions
// (tasks.md T031), per contracts/mcp-tools.md and this package's doc
// comment (types.go): plain "core" functions with the
// (*Result, *ToolError, error) three-way return, reusing the shared
// Period/ToolError/dateLayout types types.go already defines, plus a thin
// MCP handler adapter over each.
//
// Registration is deliberately NOT wired into cmd/server/main.go here: the
// in-process MCP server itself (tasks.md T020, User Story 2) doesn't exist
// yet, and wiring these tools into a server that doesn't exist would be
// scope creep into US2/US3. RegisterPromoTools is exported specifically so
// whoever builds T020 can call it directly, the same way this file's tool
// constructors and core functions are exported so they can be unit-tested
// without a live MCP server at all.
package mcptools

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/money"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/reconcile"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// PromotionRoiView is the JSON-friendly rendering of one
// reconcile.PromotionRoiRecord. roi/attributed_incremental_* are omitted
// (nil) together, with Reason set to "attribution_unavailable", when
// FR-013 applies — the tool layer enforces this the same way
// internal/reconcile and the DB CHECK constraints do (data-model.md: three
// independent gates on one invariant).
type PromotionRoiView struct {
	Platform                     string                   `json:"platform"`
	CampaignID                   string                   `json:"campaign_id"`
	Period                       Period                   `json:"period"`
	Spend                        string                   `json:"spend"`
	AttributedIncrementalOrders  *int                     `json:"attributed_incremental_orders"`
	AttributedIncrementalRevenue *string                  `json:"attributed_incremental_revenue"`
	ROI                          *string                  `json:"roi"`
	Reason                       string                   `json:"reason,omitempty"`
	FlaggedNegative              bool                     `json:"flagged_negative"`
	SourceRowRefs                []reconcile.SourceRowRef `json:"source_row_refs"`
	// Origin/ReplacesCampaignID back spec 002-badge-expansion's User Story 3:
	// "ingested" (the file pipeline, every pre-002 record) or
	// "owner_created" (POST /api/promotions), and — only on an
	// owner_created record with a replacement claim — the flagged campaign
	// it names. Rendered here so the Promotions page can show an
	// owner-logged replacement through the exact same surface as an
	// ingested campaign (FR-005's "renders through the same surfaces"),
	// distinguished rather than indistinguishable.
	Origin             string  `json:"origin"`
	ReplacesCampaignID *string `json:"replaces_campaign_id,omitempty"`
}

// PromotionRoiResult is get_promotion_roi's and
// list_negative_roi_promotions' shared success shape — both return
// PromotionRoiRecord row(s) per contracts/mcp-tools.md.
type PromotionRoiResult struct {
	Promotions []PromotionRoiView `json:"promotions"`
}

func toPromotionRoiView(rec reconcile.PromotionRoiRecord) PromotionRoiView {
	view := PromotionRoiView{
		Platform:   rec.Platform,
		CampaignID: rec.CampaignID,
		Period: Period{
			Start: rec.PeriodStart.Format(dateLayout),
			End:   rec.PeriodEnd.Format(dateLayout),
		},
		Spend:              money.FormatCents(rec.SpendCents),
		FlaggedNegative:    rec.FlaggedNegative,
		SourceRowRefs:      rec.SourceRowRefs,
		Origin:             rec.Origin,
		ReplacesCampaignID: rec.ReplacesCampaignID,
	}
	if rec.AttributedIncrementalOrders != nil {
		view.AttributedIncrementalOrders = rec.AttributedIncrementalOrders
	}
	if rec.AttributedIncrementalRevenueCents != nil {
		s := money.FormatCents(*rec.AttributedIncrementalRevenueCents)
		view.AttributedIncrementalRevenue = &s
	}
	if rec.ROICents != nil {
		s := money.FormatCents(*rec.ROICents)
		view.ROI = &s
	} else if rec.Origin == reconcile.OriginOwnerCreated {
		// A different fact from FR-013's "we tried and could not attribute"
		// — an owner-created record (spec 002 User Story 3) has never been
		// through attribution at all, since no delivery-platform data has
		// been tagged to it yet. Both render as roi: null (never a
		// computed-looking zero), but the reason distinguishes "tried,
		// failed" from "hasn't run yet" rather than overloading one string
		// for two different facts.
		view.Reason = "not_yet_attributed"
	} else {
		// FR-013, enforced at the tool boundary too: roi stays null and the
		// caller is told exactly why, never a computed-looking value.
		view.Reason = "attribution_unavailable"
	}
	return view
}

// NewPromotionRoiResult renders already-computed PromotionRoiRecords into
// the JSON-facing shape, preserving FR-013's null-roi-with-a-reason rule.
// Exported for the same reason as NewDailySummaryResult: internal/httpapi's
// GET /api/promotions serves this identical shape rather than a second,
// parallel rendering of the same records.
func NewPromotionRoiResult(records []reconcile.PromotionRoiRecord) *PromotionRoiResult {
	views := make([]PromotionRoiView, 0, len(records))
	for _, rec := range records {
		views = append(views, toPromotionRoiView(rec))
	}
	return &PromotionRoiResult{Promotions: views}
}

// --- get_promotion_roi ---

// GetPromotionRoiArgs is get_promotion_roi's input per
// contracts/mcp-tools.md: either CampaignID alone, or Platform+Period
// together.
type GetPromotionRoiArgs struct {
	CampaignID string  `json:"campaign_id,omitempty"`
	Platform   string  `json:"platform,omitempty"`
	Period     *Period `json:"period,omitempty"`
}

// GetPromotionRoi is get_promotion_roi's core function (see this package's
// doc comment in types.go for why this is split from the MCP handler
// adapter below).
func GetPromotionRoi(ctx context.Context, store storage.Querier, args GetPromotionRoiArgs) (*PromotionRoiResult, *ToolError, error) {
	switch {
	case args.CampaignID != "":
		records, err := storage.LoadPromotionRoiRecordsByCampaign(ctx, store, args.CampaignID)
		if err != nil {
			return nil, nil, err
		}
		if len(records) == 0 {
			// Exact match failed — before returning no_data, try resolving
			// args.CampaignID as a shortened form or a human-readable
			// display name against the real, bounded campaign_id set
			// (campaign_match.go). This is the fix for the "campaign
			// name/entity lookup defect" (docs/plan.md mistakes log): a
			// real, computable campaign like JET-CAMP-LUNCHFIX previously
			// produced a false no_data/refusal when asked about by its
			// full display name or as "LUNCHFIX".
			known, kerr := storage.LoadDistinctCampaignIDs(ctx, store)
			if kerr != nil {
				return nil, nil, kerr
			}
			if resolved := matchCampaignID(args.CampaignID, known); resolved != "" {
				records, err = storage.LoadPromotionRoiRecordsByCampaign(ctx, store, resolved)
				if err != nil {
					return nil, nil, err
				}
			}
		}
		if len(records) == 0 {
			return nil, &ToolError{Error: "no_data", Reason: fmt.Sprintf("no promotion found with campaign_id %q", args.CampaignID)}, nil
		}
		return NewPromotionRoiResult(records), nil, nil

	case args.Platform != "" && args.Period != nil:
		start, end, perr := args.Period.parse()
		if perr != nil {
			return nil, invalidInput(perr.Error()), nil
		}
		records, err := storage.LoadPromotionRoiRecordsByPlatformAndPeriod(ctx, store, args.Platform, start, end)
		if err != nil {
			return nil, nil, err
		}
		if len(records) == 0 {
			return nil, &ToolError{Error: "no_data", Reason: fmt.Sprintf("no promotions found for platform %q overlapping %s..%s", args.Platform, args.Period.Start, args.Period.End)}, nil
		}
		return NewPromotionRoiResult(records), nil, nil

	default:
		return nil, invalidInput("either campaign_id, or both platform and period, must be given"), nil
	}
}

// GetPromotionRoiTool is the mcp-go Tool definition for get_promotion_roi
// per contracts/mcp-tools.md.
func GetPromotionRoiTool() mcp.Tool {
	return mcp.NewTool("get_promotion_roi",
		mcp.WithDescription("Look up a promotion/ad-spend campaign's computed ROI, either by exact campaign_id or by platform+period. roi is null with reason=\"attribution_unavailable\" when incremental revenue cannot be attributed from available data (FR-013) — this is never estimated, so always check for a null roi before narrating a figure."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithString("campaign_id", mcp.Description("Campaign identifier to look up. Accepts the exact campaign_id (e.g. IFOOD-CAMP-BOOST01), a shortened form (e.g. BOOST01), or a full human-readable campaign name/description that contains the id or its shortened form (e.g. \"Banner Ad - Lunch Fix Menu (JET-CAMP-LUNCHFIX)\") — pass whatever reference was given, this tool matches it against the real known campaign set. Mutually exclusive with platform+period.")),
		mcp.WithString("platform", mcp.Description("Delivery platform name (e.g. iFood, Just Eat Takeaway). Requires period.")),
		mcp.WithObject("period",
			mcp.Description("{start, end} as YYYY-MM-DD, inclusive. Required with platform; ignored when campaign_id is given."),
			mcp.Properties(map[string]any{
				"start": map[string]any{"type": "string", "description": "YYYY-MM-DD"},
				"end":   map[string]any{"type": "string", "description": "YYYY-MM-DD"},
			}),
		),
	)
}

// GetPromotionRoiHandler adapts GetPromotionRoi into an MCP
// ToolHandlerFunc: a *ToolError becomes a structured, IsError:true result
// (this package's documented convention for an error the tool itself
// produces, per types.go); an underlying error is returned as a genuine
// protocol-level error rather than a business outcome for the model to
// narrate around.
func GetPromotionRoiHandler(store storage.Querier) server.ToolHandlerFunc {
	return mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, args GetPromotionRoiArgs) (*mcp.CallToolResult, error) {
		result, toolErr, err := GetPromotionRoi(ctx, store, args)
		if err != nil {
			return nil, err
		}
		if toolErr != nil {
			res := mcp.NewToolResultStructuredOnly(toolErr)
			res.IsError = true
			return res, nil
		}
		return mcp.NewToolResultStructuredOnly(result), nil
	})
}

// --- list_negative_roi_promotions ---

// ListNegativeRoiPromotionsArgs is list_negative_roi_promotions' input per
// contracts/mcp-tools.md.
type ListNegativeRoiPromotionsArgs struct {
	Period Period `json:"period"`
}

// ListNegativeRoiPromotions is list_negative_roi_promotions' core function —
// backs spec User Story 4 / SC-006 directly. A promotion with unattributable
// incremental revenue (FR-013) is never returned here: it isn't known to be
// negative, so it isn't flagged (this mirrors flagged_negative always being
// false when roi is nil, both in internal/reconcile and in the DB schema).
func ListNegativeRoiPromotions(ctx context.Context, store storage.Querier, args ListNegativeRoiPromotionsArgs) (*PromotionRoiResult, *ToolError, error) {
	start, end, err := args.Period.parse()
	if err != nil {
		return nil, invalidInput(err.Error()), nil
	}

	records, qerr := storage.LoadNegativeRoiPromotionsInPeriod(ctx, store, start, end)
	if qerr != nil {
		return nil, nil, qerr
	}
	return NewPromotionRoiResult(records), nil, nil
}

// ListNegativeRoiPromotionsTool is the mcp-go Tool definition for
// list_negative_roi_promotions per contracts/mcp-tools.md.
func ListNegativeRoiPromotionsTool() mcp.Tool {
	return mcp.NewTool("list_negative_roi_promotions",
		mcp.WithDescription("List every promotion/campaign whose computed ROI is negative (spend exceeded attributed incremental revenue) within a period. A promotion with unattributable incremental revenue (FR-013) is never included here."),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithObject("period",
			mcp.Required(),
			mcp.Description("{start, end} as YYYY-MM-DD, inclusive."),
			mcp.Properties(map[string]any{
				"start": map[string]any{"type": "string", "description": "YYYY-MM-DD"},
				"end":   map[string]any{"type": "string", "description": "YYYY-MM-DD"},
			}),
		),
	)
}

// ListNegativeRoiPromotionsHandler adapts ListNegativeRoiPromotions into an
// MCP ToolHandlerFunc, following the same convention as
// GetPromotionRoiHandler.
func ListNegativeRoiPromotionsHandler(store storage.Querier) server.ToolHandlerFunc {
	return mcp.NewTypedToolHandler(func(ctx context.Context, _ mcp.CallToolRequest, args ListNegativeRoiPromotionsArgs) (*mcp.CallToolResult, error) {
		result, toolErr, err := ListNegativeRoiPromotions(ctx, store, args)
		if err != nil {
			return nil, err
		}
		if toolErr != nil {
			res := mcp.NewToolResultStructuredOnly(toolErr)
			res.IsError = true
			return res, nil
		}
		return mcp.NewToolResultStructuredOnly(result), nil
	})
}

// RegisterPromoTools registers both tools on an mcp-go server. Exported so a
// later phase's server wiring (tasks.md T020) can call it directly — this
// package does not call it itself, and cmd/server/main.go does not either
// (see this file's doc comment).
func RegisterPromoTools(s *server.MCPServer, store storage.Querier) {
	s.AddTool(GetPromotionRoiTool(), GetPromotionRoiHandler(store))
	s.AddTool(ListNegativeRoiPromotionsTool(), ListNegativeRoiPromotionsHandler(store))
}
