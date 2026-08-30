package bff

// GET /api/sources — every source of data this product ingests, described
// in one vocabulary (specs/013-bff-layer FR-006, FR-007).
//
// # The defect this closes
//
// Bringing new source data into the reconciliation engine is ONE owner-facing
// job: stage something, look at it, then let it change the numbers. The
// codebase says so itself — internal/httpapi/connector_sync.go's own doc
// comment reads "mirroring ingest_cost_sheet.go's preview/commit shape
// because it is the same job".
//
// It is nonetheless exposed as two URL families with two naming schemes and
// two response vocabularies:
//
//	GET  /api/ingest/cost-sheet/template     GET  /api/connectors/platforms
//	POST /api/ingest/cost-sheet/preview      POST /api/connectors/sync/preview
//	POST /api/ingest/cost-sheet/commit       POST /api/connectors/sync
//
// The frontend pays for the split: UploadPage.tsx is ONE page with two tabs,
// and each tab was written against a different API idiom. When the product
// owner said the frontend was "reasoning about two separate backend
// concerns", this is the seam they were feeling — the concerns are not "main
// backend" and "platform connector" (those were never two services; the
// connector is an in-process Go package on the same mux), they are "upload"
// and "connect", and they are the same concern.
//
// This endpoint unifies the READ side of that job. The write paths keep
// their URLs: renaming them is the logical end of the argument and a
// breaking change to two working components, which spec 013 declined to
// spend its risk budget on. Recorded as knowingly unfinished.
//
// # Shaping, not deciding
//
// This is a BFF endpoint doing a BFF's actual job: presenting two genuinely
// different arrival mechanisms in one vocabulary WITHOUT pretending the
// difference away. That is why `kind` exists. A file upload and a connector
// pull really are different — one waits for a person, one does not — and an
// endpoint that flattened that distinction would be lying to make a list
// look tidy. It composes and shapes; it decides nothing, computes nothing,
// and persists nothing.

import (
	"net/http"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/httpapi"
	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/platformconnector"
)

// The two ways data reaches the reconciliation engine.
const (
	// SourceKindFileUpload: a person exports a file and uploads it
	// (specs/007-cost-sheet-upload).
	SourceKindFileUpload = "file_upload"
	// SourceKindConnector: the product pulls from an upstream API on
	// demand (specs/010, 012). Every connector in this product today is
	// simulated; that is a property of the SOURCE, carried per source
	// below, never of the kind.
	SourceKindConnector = "connector"
)

// SourceView is one source of ingested data.
type SourceView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`

	// Simulated and Notice are PER SOURCE, never per list, and that is
	// FR-007 rather than a style preference.
	//
	// This product discloses connector emulation in five independent
	// places by design (spec 010's honesty bar): the package doc, the
	// simulated:// provenance scheme, "simulated": true in every API body,
	// the per-source UI row, and the persistent panel notice. A uniform
	// list carrying one flag at the top would quietly become a sixth place
	// the disclosure can be cropped from — by a screenshot of one row, by
	// a client rendering a single entry, by a future integration reading
	// one object out of the array. Disclosure that survives only when the
	// whole envelope is present is disclosure that can be lost.
	//
	// The cost sheet is a real file a real person uploads, so it is not
	// marked simulated and carries no notice. Marking it simulated for
	// uniformity would be the inverse dishonesty: claiming emulation where
	// there is none devalues the claim where there is.
	Simulated bool   `json:"simulated"`
	Notice    string `json:"notice,omitempty"`

	// Arrival is one plain sentence about how this source's data reaches
	// the product, for an owner who does not know what a connector is.
	Arrival string `json:"arrival"`

	// WireFormat and CommissionRatePct are connector-only and empty for a
	// file upload — a CSV has no upstream payload shape to describe and
	// charges no commission. Carried through from the proxy's own
	// Describe() rather than retyped, so they cannot drift from what the
	// adapters actually normalize.
	WireFormat        string `json:"wire_format,omitempty"`
	CommissionRatePct string `json:"commission_rate_pct,omitempty"`
	Endpoint          string `json:"endpoint,omitempty"`
}

// SourcesResponse is GET /api/sources' body.
type SourcesResponse struct {
	Sources []SourceView `json:"sources"`
}

// costSheetSource is the one entry with no registry behind it.
//
// The three connector entries come from platformconnector.Describe(), so a
// fourth registered upstream would appear here without this file being
// edited. The cost sheet has no such registry, and inventing one purely to
// serve a description would be building a mechanism to avoid a literal.
// Stated plainly instead: this is a hand-written description, and it is the
// only one.
var costSheetSource = SourceView{
	ID:        "supplier_cost_sheet",
	Name:      "Supplier cost sheet",
	Kind:      SourceKindFileUpload,
	Simulated: false,
	Arrival:   "You export a cost sheet from your supplier or bookkeeping tool and upload the file. Preview it before it changes any numbers.",
}

// HandleSources implements GET /api/sources.
//
// No model, no arithmetic, no persistence — it reads static descriptions and
// the proxy's own registrations. There is nothing here for the
// deterministic/probabilistic boundary to be on either side of.
func HandleSources(proxy *platformconnector.Proxy) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		descriptions := proxy.Describe()
		sources := make([]SourceView, 0, len(descriptions)+1)
		sources = append(sources, costSheetSource)

		for _, d := range descriptions {
			view := SourceView{
				ID:                string(d.Platform),
				Name:              d.Name,
				Kind:              SourceKindConnector,
				Simulated:         d.Simulated,
				Arrival:           "Pulled on demand when you sync a date range. Nothing to export by hand.",
				WireFormat:        d.WireFormat,
				CommissionRatePct: d.CommissionRatePct,
				Endpoint:          d.Endpoint,
			}
			if d.Simulated {
				view.Notice = simulationNotice
			}
			sources = append(sources, view)
		}

		httpapi.WriteJSON(w, http.StatusOK, SourcesResponse{Sources: sources})
	}
}

// simulationNotice is the exact sentence every connector response body
// already carries, taken from internal/httpapi rather than restated. A
// near-copy would be two wordings of one disclosure, and the one nobody is
// looking at is the one that drifts.
const simulationNotice = httpapi.SimulationNotice
