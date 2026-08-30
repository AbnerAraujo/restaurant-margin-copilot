package bff

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/platformconnector"
)

func getSources(t *testing.T) SourcesResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	HandleSources(platformconnector.NewSimulatedProxy())(rec, httptest.NewRequest(http.MethodGet, "/api/sources", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body SourcesResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding /api/sources: %v", err)
	}
	return body
}

// TestSourcesListsEverySourceTheProductIngests covers FR-006. The defect
// (spec 013, D4) is that the two ingestion families are the SAME owner-facing
// job — connector_sync.go says so itself: "mirroring ingest_cost_sheet.go's
// preview/commit shape because it is the same job" — exposed under two URL
// prefixes with two response vocabularies. UploadPage.tsx pays for it: one
// page, two tabs, each written against a different API idiom.
func TestSourcesListsEverySourceTheProductIngests(t *testing.T) {
	body := getSources(t)

	want := map[string]string{
		"supplier_cost_sheet": SourceKindFileUpload,
		"ifood":               SourceKindConnector,
		"just_eat_takeaway":   SourceKindConnector,
		"pos":                 SourceKindConnector,
	}
	got := map[string]string{}
	for _, s := range body.Sources {
		got[s.ID] = s.Kind
	}

	for id, kind := range want {
		if got[id] != kind {
			t.Errorf("source %q has kind %q, want %q", id, got[id], kind)
		}
	}
	if len(body.Sources) != len(want) {
		t.Errorf("got %d sources, want %d: %v", len(body.Sources), len(want), got)
	}
}

// TestEverySimulatedSourceCarriesItsOwnNotice covers FR-007, and it is the
// only way this endpoint could damage something that matters.
//
// The emulation disclosure exists in five independent places by design
// (spec 010's honesty bar, spec 012's too): the package doc, the
// simulated:// provenance scheme, "simulated": true in every API body, the
// per-source UI row, and the persistent panel notice. A "tidier" uniform
// list carrying ONE flag at the top would quietly become a sixth place the
// disclosure can be cropped from — a screenshot of one row, a client
// rendering one entry, an integration reading one object. Per source or it
// does not count.
func TestEverySimulatedSourceCarriesItsOwnNotice(t *testing.T) {
	for _, s := range getSources(t).Sources {
		if !s.Simulated {
			continue
		}
		if s.Notice == "" {
			t.Errorf("simulated source %q carries no notice of its own", s.ID)
		}
		if !strings.Contains(strings.ToLower(s.Notice), "emulated") {
			t.Errorf("source %q notice does not say it is emulated: %q", s.ID, s.Notice)
		}
	}
}

// TestTheCostSheetIsNotMarkedSimulated: the supplier cost sheet is a real
// file a real person really uploads. Marking it simulated for uniformity
// would be the inverse dishonesty — disclosing emulation where there is
// none devalues the disclosure where there is.
func TestTheCostSheetIsNotMarkedSimulated(t *testing.T) {
	for _, s := range getSources(t).Sources {
		if s.ID != "supplier_cost_sheet" {
			continue
		}
		if s.Simulated {
			t.Error("the supplier cost sheet is a real uploaded file; it must not be marked simulated")
		}
		if s.Notice != "" {
			t.Errorf("the cost sheet carries an emulation notice it should not: %q", s.Notice)
		}
		return
	}
	t.Fatal("supplier_cost_sheet is missing from /api/sources")
}

// TestConnectorSourcesTrackTheProxy: the three connector entries are built
// from platformconnector's own Describe(), not retyped here, so they cannot
// drift from what the proxy actually registers. A fourth registered upstream
// would appear without this endpoint being edited.
func TestConnectorSourcesTrackTheProxy(t *testing.T) {
	proxy := platformconnector.NewSimulatedProxy()
	described := proxy.Describe()

	connectors := 0
	byID := map[string]SourceView{}
	for _, s := range getSources(t).Sources {
		if s.Kind == SourceKindConnector {
			connectors++
			byID[s.ID] = s
		}
	}
	if connectors != len(described) {
		t.Fatalf("%d connector sources listed, proxy describes %d", connectors, len(described))
	}
	for _, d := range described {
		s, ok := byID[string(d.Platform)]
		if !ok {
			t.Errorf("proxy describes %q but /api/sources does not list it", d.Platform)
			continue
		}
		if s.Name != d.Name {
			t.Errorf("%s: name = %q, proxy says %q", d.Platform, s.Name, d.Name)
		}
		if s.Simulated != d.Simulated {
			t.Errorf("%s: simulated = %v, proxy says %v", d.Platform, s.Simulated, d.Simulated)
		}
	}
}

// TestSourcesDescribesArrivalForEverySource: `kind` alone is a category;
// `arrival` is the sentence that makes the list useful to an owner who does
// not know what a connector is.
func TestSourcesDescribesArrivalForEverySource(t *testing.T) {
	for _, s := range getSources(t).Sources {
		if s.Arrival == "" {
			t.Errorf("source %q says nothing about how its data arrives", s.ID)
		}
		if s.Name == "" {
			t.Errorf("source %q has no display name", s.ID)
		}
	}
}
