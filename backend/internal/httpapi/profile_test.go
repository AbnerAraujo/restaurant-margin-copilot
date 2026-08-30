package httpapi

// Fake-store unit tests for GET/PUT /api/profile — covering every
// validation and error path (including the server-side photo size-cap
// rejection) without a live database, following business_insight_handler_test.go's
// counting/recording-fake discipline. Live-Postgres round-trip coverage
// lives in internal/storage/restaurant_profile_test.go.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// fakeProfileStore is an in-memory ProfileStore double: nil row means "no
// profile saved yet" (GetRestaurantProfile returns pgx.ErrNoRows), matching
// the real table's actual empty-database state.
type fakeProfileStore struct {
	row       *storage.RestaurantProfile
	getErr    error
	upsertErr error
	upserts   int
	lastArg   storage.UpsertRestaurantProfileParams
}

func (s *fakeProfileStore) GetRestaurantProfile(context.Context) (storage.RestaurantProfile, error) {
	if s.getErr != nil {
		return storage.RestaurantProfile{}, s.getErr
	}
	if s.row == nil {
		return storage.RestaurantProfile{}, pgx.ErrNoRows
	}
	return *s.row, nil
}

// UpsertRestaurantProfile mirrors the real query's optimistic-concurrency
// WHERE clause (see restaurant_profile.sql's doc comment): when a row
// already exists, the write only applies if arg.ExpectedUpdatedAt equals
// that row's current UpdatedAt exactly — anything else, including an
// invalid/NULL ExpectedUpdatedAt, is a mismatch and surfaces as
// pgx.ErrNoRows, same as the real ON CONFLICT ... WHERE ... RETURNING
// returning zero rows. A row that doesn't exist yet is a plain insert, no
// check needed, matching Postgres's own INSERT-vs-ON-CONFLICT branching.
func (s *fakeProfileStore) UpsertRestaurantProfile(_ context.Context, arg storage.UpsertRestaurantProfileParams) (storage.RestaurantProfile, error) {
	s.upserts++
	s.lastArg = arg
	if s.upsertErr != nil {
		return storage.RestaurantProfile{}, s.upsertErr
	}
	if s.row != nil {
		current := s.row.UpdatedAt
		if !arg.ExpectedUpdatedAt.Valid || !current.Valid || !arg.ExpectedUpdatedAt.Time.Equal(current.Time) {
			return storage.RestaurantProfile{}, pgx.ErrNoRows
		}
	}
	row := storage.RestaurantProfile{
		ID:               1,
		Name:             arg.Name,
		Address:          arg.Address,
		Phone:            arg.Phone,
		Email:            arg.Email,
		Description:      arg.Description,
		PhotoData:        arg.PhotoData,
		PhotoContentType: arg.PhotoContentType,
		UpdatedAt:        pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	s.row = &row
	return row, nil
}

func doGetProfile(store *fakeProfileStore) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/profile", nil)
	HandleProfile(store)(rec, req)
	return rec
}

func doPutProfile(store *fakeProfileStore, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/profile", strings.NewReader(body))
	HandleProfile(store)(rec, req)
	return rec
}

func TestHandleProfile_GetWithNoRowSavedYetReturnsEmptyProfile(t *testing.T) {
	store := &fakeProfileStore{}
	rec := doGetProfile(store)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var view ProfileView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if view.Name != "" || view.Photo != nil || view.UpdatedAt != "" {
		t.Errorf("view = %+v, want an all-empty first-run profile, not an error", view)
	}
}

func TestHandleProfile_GetQueryFailurePropagatesAs500(t *testing.T) {
	store := &fakeProfileStore{getErr: errors.New("connection reset")}
	rec := doGetProfile(store)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestHandleProfile_GetRendersSavedRowIncludingPhotoDataURI(t *testing.T) {
	photoBytes := []byte{0x89, 'P', 'N', 'G', 1, 2, 3}
	store := &fakeProfileStore{row: &storage.RestaurantProfile{
		Name:             "Trattoria Bellavista",
		Address:          "123 Main St",
		Phone:            "+1 555 123 4567",
		Email:            "owner@bellavista.example",
		Description:      "Family-run Italian kitchen since 1998.",
		PhotoData:        photoBytes,
		PhotoContentType: pgtype.Text{String: "image/png", Valid: true},
		UpdatedAt:        pgtype.Timestamptz{Valid: true},
	}}

	rec := doGetProfile(store)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var view ProfileView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if view.Name != "Trattoria Bellavista" {
		t.Errorf("name = %q", view.Name)
	}
	wantPhoto := "data:image/png;base64," + base64.StdEncoding.EncodeToString(photoBytes)
	if view.Photo == nil || *view.Photo != wantPhoto {
		t.Errorf("photo = %v, want %q", view.Photo, wantPhoto)
	}
}

func TestHandleProfile_PutRejectsWrongMethod(t *testing.T) {
	store := &fakeProfileStore{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/profile", nil)
	HandleProfile(store)(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestHandleProfile_PutRequiresName(t *testing.T) {
	store := &fakeProfileStore{}
	rec := doPutProfile(store, `{"name":"  ","address":"123 Main St"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_input") {
		t.Errorf("body = %s, want invalid_input error code", rec.Body.String())
	}
	if store.upserts != 0 {
		t.Errorf("upserts = %d, want 0 — an invalid request must never reach the store", store.upserts)
	}
}

func TestHandleProfile_PutRejectsFieldTooLong(t *testing.T) {
	store := &fakeProfileStore{}
	longName := strings.Repeat("a", profileNameMaxLen+1)
	rec := doPutProfile(store, `{"name":"`+longName+`"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "too long") {
		t.Errorf("body = %s, want a clear too-long message", rec.Body.String())
	}
}

func TestHandleProfile_PutRejectsInvalidEmail(t *testing.T) {
	store := &fakeProfileStore{}
	rec := doPutProfile(store, `{"name":"Cafe Test","email":"not-an-email"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestHandleProfile_PutRejectsInvalidPhone(t *testing.T) {
	store := &fakeProfileStore{}
	rec := doPutProfile(store, `{"name":"Cafe Test","phone":"call-us-maybe"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestHandleProfile_PutAcceptsValidProfileWithPhotoAndUpserts(t *testing.T) {
	store := &fakeProfileStore{}
	photoBytes := []byte("tiny-fake-png-bytes")
	dataURI := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(photoBytes)

	body, err := json.Marshal(ProfileRequest{
		Name:        "Trattoria Bellavista",
		Address:     "123 Main St, Springfield",
		Phone:       "+1 (555) 123-4567",
		Email:       "owner@bellavista.example",
		Description: "Family-run Italian kitchen since 1998.",
		Photo:       &dataURI,
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	rec := doPutProfile(store, string(body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if store.upserts != 1 {
		t.Fatalf("upserts = %d, want exactly 1", store.upserts)
	}
	if store.lastArg.Name != "Trattoria Bellavista" || store.lastArg.Email != "owner@bellavista.example" {
		t.Errorf("upsert arg = %+v, want the submitted fields", store.lastArg)
	}
	if !bytes.Equal(store.lastArg.PhotoData, photoBytes) {
		t.Errorf("upsert photo bytes = %v, want %v (decoded from the data URI)", store.lastArg.PhotoData, photoBytes)
	}
	if store.lastArg.PhotoContentType.String != "image/jpeg" {
		t.Errorf("upsert photo content type = %q, want image/jpeg", store.lastArg.PhotoContentType.String)
	}

	var view ProfileView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if view.Photo == nil || *view.Photo != dataURI {
		t.Errorf("response photo = %v, want %q", view.Photo, dataURI)
	}
}

func TestHandleProfile_PutOmittingPhotoClearsIt(t *testing.T) {
	loadedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	store := &fakeProfileStore{row: &storage.RestaurantProfile{
		Name:             "Old Name",
		PhotoData:        []byte("existing-photo"),
		PhotoContentType: pgtype.Text{String: "image/png", Valid: true},
		UpdatedAt:        pgtype.Timestamptz{Time: loadedAt, Valid: true},
	}}

	rec := doPutProfile(store, `{"name":"New Name","updated_at":"`+loadedAt.Format(time.RFC3339Nano)+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if store.lastArg.PhotoData != nil || store.lastArg.PhotoContentType.Valid {
		t.Errorf("upsert arg photo = %v/%v, want cleared (PUT is a full replace)", store.lastArg.PhotoData, store.lastArg.PhotoContentType)
	}
}

func TestHandleProfile_PutRejectsMalformedPhotoDataURI(t *testing.T) {
	store := &fakeProfileStore{}
	rec := doPutProfile(store, `{"name":"Cafe Test","photo":"not-a-data-uri"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_photo") {
		t.Errorf("body = %s, want invalid_photo error code", rec.Body.String())
	}
	if store.upserts != 0 {
		t.Errorf("upserts = %d, want 0", store.upserts)
	}
}

func TestHandleProfile_PutRejectsUnsupportedPhotoType(t *testing.T) {
	store := &fakeProfileStore{}
	dataURI := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte("<svg></svg>"))
	rec := doPutProfile(store, `{"name":"Cafe Test","photo":"`+dataURI+`"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unsupported_photo_type") {
		t.Errorf("body = %s, want unsupported_photo_type error code", rec.Body.String())
	}
}

// TestHandleProfile_PutRejectsOversizedPhoto is the required size-cap
// rejection test: a photo decoding to just over maxProfilePhotoBytes must
// be refused server-side (413), never trusted on the strength of a
// client-side check alone.
func TestHandleProfile_PutRejectsOversizedPhoto(t *testing.T) {
	store := &fakeProfileStore{}
	oversized := make([]byte, maxProfilePhotoBytes+1024)
	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(oversized)

	body, err := json.Marshal(ProfileRequest{Name: "Cafe Test", Photo: &dataURI})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	rec := doPutProfile(store, string(body))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (body: %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "photo_too_large") {
		t.Errorf("body = %s, want photo_too_large error code", rec.Body.String())
	}
	if store.upserts != 0 {
		t.Errorf("upserts = %d, want 0 — an oversized photo must never reach the store", store.upserts)
	}
}

func TestHandleProfile_PutRejectsRequestBodyOverTheOuterCap(t *testing.T) {
	store := &fakeProfileStore{}
	// Bigger than maxProfileRequestBodyBytes even before JSON/base64
	// overhead — must be refused by http.MaxBytesReader before the photo's
	// own size check ever runs.
	huge := strings.Repeat("a", maxProfileRequestBodyBytes+1024)
	rec := doPutProfile(store, `{"name":"`+huge+`"}`)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (body: %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "request_too_large") {
		t.Errorf("body = %s, want request_too_large error code", rec.Body.String())
	}
}

func TestHandleProfile_PutUpsertFailurePropagatesAs500(t *testing.T) {
	store := &fakeProfileStore{upsertErr: errors.New("connection reset")}
	rec := doPutProfile(store, `{"name":"Cafe Test"}`)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestHandleProfile_PutRejectsInvalidJSON(t *testing.T) {
	store := &fakeProfileStore{}
	rec := doPutProfile(store, `{not json`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid_body") {
		t.Errorf("body = %s, want invalid_body error code", rec.Body.String())
	}
}

// TestHandleProfile_PutRejectsStaleUpdatedAtWithConflict is the QA
// two-tab lost-update fix's core proof: a PUT whose updated_at no longer
// matches the row's current value (because a save from elsewhere landed in
// between) must be refused with 409, never silently applied over that
// newer save.
func TestHandleProfile_PutRejectsStaleUpdatedAtWithConflict(t *testing.T) {
	originallyLoadedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	someoneElseSavedAt := time.Date(2026, 8, 20, 12, 5, 0, 0, time.UTC)
	store := &fakeProfileStore{row: &storage.RestaurantProfile{
		Name:      "Whatever Someone Else Just Saved",
		Phone:     "+1 555 999 0000",
		UpdatedAt: pgtype.Timestamptz{Time: someoneElseSavedAt, Valid: true},
	}}

	// This tab loaded the profile back when updated_at was still
	// originallyLoadedAt, and is now trying to save unaware that another
	// tab has since saved (moving updated_at to someoneElseSavedAt).
	rec := doPutProfile(store, `{"name":"Stale Tab Address Edit","updated_at":"`+originallyLoadedAt.Format(time.RFC3339Nano)+`"}`)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body: %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "profile_conflict") {
		t.Errorf("body = %s, want profile_conflict error code", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "updated elsewhere") {
		t.Errorf("body = %s, want a clear explanation that the profile changed elsewhere", rec.Body.String())
	}
	if store.row.Name != "Whatever Someone Else Just Saved" {
		t.Errorf("row.Name = %q, want the other tab's save left untouched — a 409 must never apply the stale write", store.row.Name)
	}
}

// TestHandleProfile_PutWithEmptyUpdatedAtConflictsIfProfileAlreadyExists
// covers the other half of the mismatch: a client that loaded the profile
// before it had ever been saved (updated_at == "") must still be refused
// if, by the time it saves, someone else has since created the profile —
// an empty/NULL expectation must never be treated as "no check needed"
// once a real row exists.
func TestHandleProfile_PutWithEmptyUpdatedAtConflictsIfProfileAlreadyExists(t *testing.T) {
	store := &fakeProfileStore{row: &storage.RestaurantProfile{
		Name:      "Someone Beat You To The First Save",
		UpdatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}}

	rec := doPutProfile(store, `{"name":"My First Save"}`)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestHandleProfile_PutRejectsMalformedUpdatedAt proves a garbled
// updated_at (never something a real client sends, but never trusted
// blindly either) is a clean 400, not a panic or a silently-ignored check.
func TestHandleProfile_PutRejectsMalformedUpdatedAt(t *testing.T) {
	store := &fakeProfileStore{}
	rec := doPutProfile(store, `{"name":"Cafe Test","updated_at":"not-a-timestamp"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
	if store.upserts != 0 {
		t.Errorf("upserts = %d, want 0 — a malformed updated_at must never reach the store", store.upserts)
	}
}
