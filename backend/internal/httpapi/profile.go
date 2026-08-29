package httpapi

// GET/PUT /api/profile: the restaurant owner's own company information and
// photo, shown on and edited from the new Profile page. Like
// promotions_create.go's write path, this is a genuine CRUD surface, not an
// MCP tool — the model never reads or writes this data, so it stays outside
// the Principle III tool boundary entirely.
//
// restaurant_profile (migration 000011) is pinned to exactly one row: this
// is a single-tenant prototype, so "the profile" always means the one
// restaurant this instance belongs to. GET before any PUT has ever
// succeeded returns a well-formed, all-empty profile (200), not a 404 —
// the Profile page's form has a real, typed shape to render into from the
// very first load, matching a settings-page convention rather than a
// resource-lookup one.
//
// The photo travels over the wire as a data URI
// ("data:image/png;base64,...") in both directions and is stored in
// Postgres as raw bytes (bytea) plus its content type — never as an
// external object-storage reference, matching this project's "no cloud
// storage dependency anywhere" constraint. PUT is a full replace of the
// one row (no partial-patch semantics): a client that wants to keep the
// existing photo must resubmit the exact data URI GET /api/profile just
// handed it; omitting or nulling the field clears the photo.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// maxProfilePhotoBytes bounds the DECODED photo this feature will persist —
// a "few MB" ceiling per the take-home brief. 5MB is generous headroom for
// a real phone-camera photo of a restaurant's storefront or dish (typical
// compressed JPEGs from a phone run a few hundred KB to low single-digit
// MB) while still being a real, enforced bound rather than trusting
// whatever size the client claims.
const maxProfilePhotoBytes = 5 << 20

// maxProfileRequestBodyBytes bounds the whole JSON request body PUT
// /api/profile will read, enforced via http.MaxBytesReader before any
// parsing happens. Base64 inflates the photo's raw bytes by roughly a
// third, plus the surrounding JSON and data-URI header — 8MB comfortably
// covers a maxProfilePhotoBytes-sized photo with room to spare, while still
// refusing a wildly oversized body outright instead of buffering it.
const maxProfileRequestBodyBytes = 8 << 20

// profileNameMaxLen and friends bound the plain-text fields — generous for
// real restaurant data, but real, enforced limits rather than "however
// large the client feels like sending" (the same posture
// maxProfilePhotoBytes applies to the photo).
const (
	profileNameMaxLen        = 200
	profileAddressMaxLen     = 300
	profilePhoneMaxLen       = 40
	profileEmailMaxLen       = 254
	profileDescriptionMaxLen = 1000
)

// allowedProfilePhotoTypes is the closed set of image content types this
// endpoint accepts — the common web-safe formats a phone or browser file
// picker produces. A closed set on purpose, matching this codebase's
// general "closed set over free text" discipline for anything that gets
// persisted (e.g. business_insight_interaction's CHECK-constrained kind).
var allowedProfilePhotoTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
}

// ProfileView is GET/PUT /api/profile's shared response shape.
type ProfileView struct {
	Name        string `json:"name"`
	Address     string `json:"address"`
	Phone       string `json:"phone"`
	Email       string `json:"email"`
	Description string `json:"description"`
	// Photo is a data URI ("data:image/png;base64,...") or null when no
	// photo has been uploaded — never a bare base64 blob, so the frontend
	// can drop it straight into an <img src>.
	Photo *string `json:"photo"`
	// UpdatedAt is RFC 3339, or "" when the profile has never been saved —
	// a real absence, not a fabricated timestamp.
	UpdatedAt string `json:"updated_at"`
}

// ProfileRequest is PUT /api/profile's request body — a full replace of
// the one profile row (see file doc comment).
type ProfileRequest struct {
	Name        string  `json:"name"`
	Address     string  `json:"address"`
	Phone       string  `json:"phone"`
	Email       string  `json:"email"`
	Description string  `json:"description"`
	Photo       *string `json:"photo"`
}

// ProfileStore is the two calls this handler needs — a narrow interface
// (the same discipline BusinessInsightStore documents) so tests can fake it
// without a live database. *storage.Queries satisfies it directly.
type ProfileStore interface {
	GetRestaurantProfile(ctx context.Context) (storage.RestaurantProfile, error)
	UpsertRestaurantProfile(ctx context.Context, arg storage.UpsertRestaurantProfileParams) (storage.RestaurantProfile, error)
}

// HandleProfile implements GET and PUT /api/profile.
func HandleProfile(store ProfileStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleGetProfile(w, r, store)
		case http.MethodPut:
			handlePutProfile(w, r, store)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET and PUT are supported")
		}
	}
}

func handleGetProfile(w http.ResponseWriter, r *http.Request, store ProfileStore) {
	row, err := store.GetRestaurantProfile(r.Context())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// No profile saved yet — a real, expected first-run state, not
			// an error (see file doc comment).
			writeJSON(w, http.StatusOK, ProfileView{Photo: nil})
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, renderProfileView(row))
}

func handlePutProfile(w http.ResponseWriter, r *http.Request, store ProfileStore) {
	r.Body = http.MaxBytesReader(w, r.Body, maxProfileRequestBodyBytes)

	var req ProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "request_too_large",
				fmt.Sprintf("the request body is too large (over %d MB) — a photo that big won't be accepted; use a smaller image", maxProfileRequestBodyBytes>>20))
			return
		}
		writeJSONError(w, http.StatusBadRequest, "invalid_body", fmt.Sprintf("could not parse request body as JSON: %v", err))
		return
	}

	name := strings.TrimSpace(req.Name)
	address := strings.TrimSpace(req.Address)
	phone := strings.TrimSpace(req.Phone)
	email := strings.TrimSpace(req.Email)
	description := strings.TrimSpace(req.Description)

	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid_input", "enter your restaurant's name — it's shown throughout the app and can't be blank")
		return
	}
	if err := checkFieldLength("name", name, profileNameMaxLen); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	if err := checkFieldLength("address", address, profileAddressMaxLen); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	if err := checkFieldLength("phone", phone, profilePhoneMaxLen); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	if err := checkFieldLength("email", email, profileEmailMaxLen); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	if err := checkFieldLength("description", description, profileDescriptionMaxLen); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	if phone != "" && !looksLikePhoneNumber(phone) {
		writeJSONError(w, http.StatusBadRequest, "invalid_input", "enter a valid phone number, using only digits, spaces, and + - ( )")
		return
	}
	if email != "" && !looksLikeEmail(email) {
		writeJSONError(w, http.StatusBadRequest, "invalid_input", "enter a valid email address, like name@restaurant.com")
		return
	}

	photoData, photoContentType, photoErr := decodeProfilePhoto(req.Photo)
	if photoErr != nil {
		writeJSONError(w, photoErr.status, photoErr.code, photoErr.detail)
		return
	}

	saved, err := store.UpsertRestaurantProfile(r.Context(), storage.UpsertRestaurantProfileParams{
		Name:             name,
		Address:          address,
		Phone:            phone,
		Email:            email,
		Description:      description,
		PhotoData:        photoData,
		PhotoContentType: photoContentType,
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, renderProfileView(saved))
}

// checkFieldLength returns a clear, actionable error — what's wrong, why,
// and how to fix it — rather than a bare "too long".
func checkFieldLength(field, value string, max int) error {
	if len(value) <= max {
		return nil
	}
	return fmt.Errorf("%s is too long (%d characters) — keep it under %d characters", field, len(value), max)
}

// looksLikePhoneNumber is a permissive sanity check, not a strict format
// validator: real restaurant phone numbers vary by country (spacing,
// parentheses, extensions), so this only rejects input that could not
// plausibly be a phone number (letters, symbols other than the ones a
// phone number actually uses) rather than enforcing one specific format.
func looksLikePhoneNumber(phone string) bool {
	digitCount := 0
	for _, r := range phone {
		switch {
		case r >= '0' && r <= '9':
			digitCount++
		case r == ' ' || r == '+' || r == '-' || r == '(' || r == ')' || r == '.' || r == 'x' || r == 'X':
			// allowed separators/extension marker
		default:
			return false
		}
	}
	return digitCount >= 6
}

// looksLikeEmail is a minimal, deliberately permissive shape check
// (local@domain.tld) — this product never sends mail to it, so the only
// goal is catching obvious typos before they're saved, not RFC 5322
// compliance.
func looksLikeEmail(email string) bool {
	at := strings.IndexByte(email, '@')
	if at <= 0 || at == len(email)-1 {
		return false
	}
	domain := email[at+1:]
	if strings.ContainsAny(email[:at], " \t") || strings.ContainsAny(domain, " \t") {
		return false
	}
	dot := strings.LastIndexByte(domain, '.')
	return dot > 0 && dot < len(domain)-1
}

// profileFieldError carries an HTTP status alongside the stable error code
// and detail message, since decodeProfilePhoto's failure modes span both
// 400 (malformed) and 413 (too large).
type profileFieldError struct {
	status int
	code   string
	detail string
}

// decodeProfilePhoto parses req.Photo (nil/empty clears the photo) into the
// raw bytes and content type restaurant_profile stores, enforcing
// maxProfilePhotoBytes and allowedProfilePhotoTypes SERVER-SIDE — the
// client's own size/type check (ProfilePage.tsx) is a UX convenience, never
// trusted on its own.
func decodeProfilePhoto(photo *string) ([]byte, pgtype.Text, *profileFieldError) {
	if photo == nil || *photo == "" {
		return nil, pgtype.Text{Valid: false}, nil
	}

	const prefix = "data:"
	if !strings.HasPrefix(*photo, prefix) {
		return nil, pgtype.Text{}, &profileFieldError{http.StatusBadRequest, "invalid_photo",
			"the photo wasn't in a format the server understands — try choosing the file again"}
	}
	header, encoded, found := strings.Cut((*photo)[len(prefix):], ",")
	if !found {
		return nil, pgtype.Text{}, &profileFieldError{http.StatusBadRequest, "invalid_photo",
			"the photo wasn't in a format the server understands — try choosing the file again"}
	}
	contentType, isBase64, found := strings.Cut(header, ";")
	if !found || isBase64 != "base64" {
		return nil, pgtype.Text{}, &profileFieldError{http.StatusBadRequest, "invalid_photo",
			"the photo wasn't in a format the server understands — try choosing the file again"}
	}
	if !allowedProfilePhotoTypes[contentType] {
		return nil, pgtype.Text{}, &profileFieldError{http.StatusBadRequest, "unsupported_photo_type",
			fmt.Sprintf("photos must be PNG, JPEG, or WebP — %q isn't supported", contentType)}
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, pgtype.Text{}, &profileFieldError{http.StatusBadRequest, "invalid_photo",
			"the photo data was corrupted in transit — try choosing the file again"}
	}
	if len(data) > maxProfilePhotoBytes {
		return nil, pgtype.Text{}, &profileFieldError{http.StatusRequestEntityTooLarge, "photo_too_large",
			fmt.Sprintf("that photo is %.1fMB, which is over the %dMB limit — choose a smaller image or compress it first",
				float64(len(data))/(1<<20), maxProfilePhotoBytes>>20)}
	}

	return data, pgtype.Text{String: contentType, Valid: true}, nil
}

// renderProfileView converts a storage row into the wire shape, rebuilding
// the photo's data URI from its stored bytes + content type.
func renderProfileView(row storage.RestaurantProfile) ProfileView {
	view := ProfileView{
		Name:        row.Name,
		Address:     row.Address,
		Phone:       row.Phone,
		Email:       row.Email,
		Description: row.Description,
	}
	if len(row.PhotoData) > 0 && row.PhotoContentType.Valid {
		dataURI := fmt.Sprintf("data:%s;base64,%s", row.PhotoContentType.String, base64.StdEncoding.EncodeToString(row.PhotoData))
		view.Photo = &dataURI
	}
	if row.UpdatedAt.Valid {
		view.UpdatedAt = row.UpdatedAt.Time.Format(time.RFC3339)
	}
	return view
}
