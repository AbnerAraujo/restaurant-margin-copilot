package storage_test

// Live-Postgres integration tests for restaurant_profile (migration
// 000011) — the singleton row backing GET/PUT /api/profile.
// connectOrSkip follows badge_expansion_test.go's own established pattern
// exactly: skipped, not faked, when DATABASE_URL is unset.
//
// This table is a singleton (id pinned to 1 — there's no sentinel id to
// isolate a test's writes onto the way business_insight_test.go's sentinel
// advice_text does), and every test here is destructive (DELETE / an INSERT
// expected to violate a CHECK constraint) against the SAME row a real owner
// may have already saved through the app. Running `go test ./...` against
// DATABASE_URL pointed at a live dev database — this project's own everyday
// workflow — must never silently wipe that row. Each test below therefore
// calls captureAndRestoreRestaurantProfile before touching the table: it
// snapshots whatever's at id=1 (or records that nothing is), and registers
// a t.Cleanup that restores it byte-for-byte once the test's own assertions
// are done.
import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

// captureAndRestoreRestaurantProfile snapshots the current restaurant_profile
// row at id=1, if any, and registers a t.Cleanup that puts it back exactly
// as it was. Call this BEFORE a test does anything destructive to the table,
// and before the test registers any t.Cleanup of its own: t.Cleanup runs
// LIFO (docs/plan.md's mistakes-log entry on cleanup ordering), so
// registering the restore first makes it run LAST — after the test's own
// cleanup has already torn down whatever it wrote — which is what actually
// puts the real row back rather than having it clobbered again.
func captureAndRestoreRestaurantProfile(t *testing.T, conn *pgx.Conn, ctx context.Context) {
	t.Helper()

	var original storage.RestaurantProfile
	err := conn.QueryRow(ctx, `
		SELECT id, name, address, phone, email, description, photo_data,
		       photo_content_type, created_at, updated_at
		FROM restaurant_profile WHERE id = 1
	`).Scan(
		&original.ID, &original.Name, &original.Address, &original.Phone,
		&original.Email, &original.Description, &original.PhotoData,
		&original.PhotoContentType, &original.CreatedAt, &original.UpdatedAt,
	)
	existed := true
	if err == pgx.ErrNoRows {
		existed = false
	} else {
		require.NoError(t, err, "must be able to read the pre-test restaurant_profile state before running a destructive test")
	}

	t.Cleanup(func() {
		_, err := conn.Exec(ctx, "DELETE FROM restaurant_profile WHERE id = 1")
		require.NoError(t, err)

		if !existed {
			return
		}

		_, err = conn.Exec(ctx, `
			INSERT INTO restaurant_profile
				(id, name, address, phone, email, description, photo_data,
				 photo_content_type, created_at, updated_at)
			VALUES (1, $1, $2, $3, $4, $5, $6, $7, $8, $9)
		`,
			original.Name, original.Address, original.Phone, original.Email,
			original.Description, original.PhotoData, original.PhotoContentType,
			original.CreatedAt, original.UpdatedAt,
		)
		require.NoError(t, err, "must restore the real pre-test restaurant_profile row — a full test-suite run against a live DATABASE_URL must never lose it")
	})
}

func TestGetRestaurantProfile_ReturnsErrNoRowsWhenNeverSaved(t *testing.T) {
	conn, q, ctx := connectOrSkip(t)
	captureAndRestoreRestaurantProfile(t, conn, ctx)

	_, err := conn.Exec(ctx, "DELETE FROM restaurant_profile WHERE id = 1")
	require.NoError(t, err)

	_, err = q.GetRestaurantProfile(ctx)
	require.ErrorIs(t, err, pgx.ErrNoRows, "an empty table must surface as ErrNoRows, not a fabricated zero-value row")
}

func TestUpsertRestaurantProfile_RoundTripsFieldsAndPhoto(t *testing.T) {
	conn, q, ctx := connectOrSkip(t)
	captureAndRestoreRestaurantProfile(t, conn, ctx)

	_, err := conn.Exec(ctx, "DELETE FROM restaurant_profile WHERE id = 1")
	require.NoError(t, err)

	photo := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}

	created, err := q.UpsertRestaurantProfile(ctx, storage.UpsertRestaurantProfileParams{
		Name:             "TEST-SENTINEL Trattoria Bellavista",
		Address:          "123 Main St, Springfield",
		Phone:            "+1 555 123 4567",
		Email:            "owner@bellavista.example",
		Description:      "Family-run Italian kitchen since 1998.",
		PhotoData:        photo,
		PhotoContentType: pgtype.Text{String: "image/png", Valid: true},
		// No row exists yet — matches a client that GET'd the empty
		// first-run profile (updated_at == "") — so the INSERT branch
		// applies unconditionally regardless of ExpectedUpdatedAt.
		ExpectedUpdatedAt: pgtype.Timestamptz{Valid: false},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, created.ID, "the table is pinned to exactly one row, id=1")
	require.Equal(t, "TEST-SENTINEL Trattoria Bellavista", created.Name)
	require.Equal(t, photo, created.PhotoData)
	require.True(t, created.PhotoContentType.Valid)
	require.Equal(t, "image/png", created.PhotoContentType.String)
	require.True(t, created.UpdatedAt.Valid)

	fetched, err := q.GetRestaurantProfile(ctx)
	require.NoError(t, err)
	require.Equal(t, created.Name, fetched.Name)
	require.Equal(t, created.PhotoData, fetched.PhotoData)

	// A second PUT (ON CONFLICT DO UPDATE) replaces the row in place — the
	// singleton stays id=1, and a cleared photo actually clears, proving
	// PUT's full-replace semantics all the way down to the database. It
	// passes back exactly the updated_at `created` just returned, matching
	// a real client that read it from the first PUT's response — this is
	// the legitimate, non-conflicting case for the optimistic-concurrency
	// WHERE clause.
	updated, err := q.UpsertRestaurantProfile(ctx, storage.UpsertRestaurantProfileParams{
		Name:              "TEST-SENTINEL Trattoria Bellavista Renamed",
		Address:           "456 Side St",
		Phone:             "",
		Email:             "",
		Description:       "",
		PhotoData:         nil,
		PhotoContentType:  pgtype.Text{Valid: false},
		ExpectedUpdatedAt: created.UpdatedAt,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, updated.ID, "still exactly one row, not a second insert")
	require.Equal(t, "TEST-SENTINEL Trattoria Bellavista Renamed", updated.Name)
	require.Nil(t, updated.PhotoData, "a cleared photo must round-trip as NULL, not stale bytes")
	require.False(t, updated.PhotoContentType.Valid)

	count, err := conn.Query(ctx, "SELECT COUNT(*) FROM restaurant_profile")
	require.NoError(t, err)
	defer count.Close()
	require.True(t, count.Next())
	var n int
	require.NoError(t, count.Scan(&n))
	require.Equal(t, 1, n, "the singleton CHECK/PK must keep this table at exactly one row")
}

// TestUpsertRestaurantProfile_StaleExpectedUpdatedAtIsRejected proves the
// optimistic-concurrency WHERE clause against a REAL Postgres instance, not
// just the fake store in internal/httpapi: a second UpsertRestaurantProfile
// call whose ExpectedUpdatedAt no longer matches the row's current
// updated_at (because a first call already moved it on) must apply
// nothing and surface as pgx.ErrNoRows — exactly the two-tab lost-update
// scenario QA found, reproduced at the SQL layer.
func TestUpsertRestaurantProfile_StaleExpectedUpdatedAtIsRejected(t *testing.T) {
	conn, q, ctx := connectOrSkip(t)
	captureAndRestoreRestaurantProfile(t, conn, ctx)

	_, err := conn.Exec(ctx, "DELETE FROM restaurant_profile WHERE id = 1")
	require.NoError(t, err)

	// Tab 1 loads the empty profile, then saves — establishing the row and
	// its first real updated_at.
	tab1Save, err := q.UpsertRestaurantProfile(ctx, storage.UpsertRestaurantProfileParams{
		Name:              "TEST-SENTINEL Tab One's Restaurant",
		Phone:             "+1 555 000 0001",
		ExpectedUpdatedAt: pgtype.Timestamptz{Valid: false},
	})
	require.NoError(t, err)

	// Tab 2 loaded the SAME empty profile as tab 1 (before either had
	// saved), so it also believes there is no row yet — but by the time it
	// saves, tab 1 has already created one. This must conflict, never
	// silently overwrite tab 1's save.
	_, err = q.UpsertRestaurantProfile(ctx, storage.UpsertRestaurantProfileParams{
		Name:              "TEST-SENTINEL Tab Two's Overwrite",
		Address:           "Tab two's address edit",
		ExpectedUpdatedAt: pgtype.Timestamptz{Valid: false},
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "a stale/empty ExpectedUpdatedAt against an existing row must be refused, not silently applied")

	// Tab 1's save must be untouched by tab 2's rejected attempt.
	current, err := q.GetRestaurantProfile(ctx)
	require.NoError(t, err)
	require.Equal(t, "TEST-SENTINEL Tab One's Restaurant", current.Name, "a rejected stale write must never overwrite the real current row")

	// A write whose ExpectedUpdatedAt DOES still match the row's real
	// current value must succeed — proving the check isn't overzealous,
	// only genuinely stale writes are refused.
	tab1SecondSave, err := q.UpsertRestaurantProfile(ctx, storage.UpsertRestaurantProfileParams{
		Name:              "TEST-SENTINEL Tab One's Second Edit",
		ExpectedUpdatedAt: tab1Save.UpdatedAt,
	})
	require.NoError(t, err, "a write whose ExpectedUpdatedAt still matches the row's real current value must succeed")

	// And now tab 1's OWN first updated_at (from its very first save) is
	// stale too, since its second save just moved updated_at on again —
	// same rejection, proving the check applies to every write, not just
	// the "row didn't exist yet" case.
	_, err = q.UpsertRestaurantProfile(ctx, storage.UpsertRestaurantProfileParams{
		Name:              "TEST-SENTINEL Stale Retry Of An Old Save",
		ExpectedUpdatedAt: tab1Save.UpdatedAt,
	})
	require.ErrorIs(t, err, pgx.ErrNoRows, "retrying a write with an updated_at that's since been superseded must still be refused")

	// Confirm tab 1's second save is the one that actually stuck.
	final, err := q.GetRestaurantProfile(ctx)
	require.NoError(t, err)
	require.Equal(t, tab1SecondSave.Name, final.Name)
}

func TestRestaurantProfile_PhotoDataAndContentTypeMustBeSetTogether(t *testing.T) {
	// Proves the restaurant_profile_photo_pair CHECK constraint from
	// migration 000011 is real, not just a comment: a raw INSERT with one
	// half of the pair set and the other NULL must be rejected by the
	// database itself.
	conn, _, ctx := connectOrSkip(t)
	captureAndRestoreRestaurantProfile(t, conn, ctx)

	_, err := conn.Exec(ctx, "DELETE FROM restaurant_profile WHERE id = 1")
	require.NoError(t, err)

	_, err = conn.Exec(ctx, `
		INSERT INTO restaurant_profile (id, name, photo_data, photo_content_type)
		VALUES (1, 'TEST-SENTINEL Half Photo', '\x89504e47', NULL)
	`)
	require.Error(t, err, "photo_data set without photo_content_type must violate the CHECK constraint")
	require.Contains(t, err.Error(), "check", "want a constraint violation, got: %v", err)
}
