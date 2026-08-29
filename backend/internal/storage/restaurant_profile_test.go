package storage_test

// Live-Postgres integration tests for restaurant_profile (migration
// 000011) — the singleton row backing GET/PUT /api/profile. Follows
// badge_expansion_test.go's own established pattern exactly: skipped, not
// faked, when DATABASE_URL is unset. Every test cleans up by deleting the
// row it created/touched (id=1 is the only row this table ever has), so
// runs never interfere with each other or leave the table non-empty for a
// subsequent local run.

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/AbnerAraujo/restaurant-margin-copilot/backend/internal/storage"
)

func TestGetRestaurantProfile_ReturnsErrNoRowsWhenNeverSaved(t *testing.T) {
	conn, q, ctx := connectOrSkip(t)

	_, err := conn.Exec(ctx, "DELETE FROM restaurant_profile WHERE id = 1")
	require.NoError(t, err)

	_, err = q.GetRestaurantProfile(ctx)
	require.ErrorIs(t, err, pgx.ErrNoRows, "an empty table must surface as ErrNoRows, not a fabricated zero-value row")
}

func TestUpsertRestaurantProfile_RoundTripsFieldsAndPhoto(t *testing.T) {
	conn, q, ctx := connectOrSkip(t)

	t.Cleanup(func() {
		_, err := conn.Exec(ctx, "DELETE FROM restaurant_profile WHERE id = 1")
		require.NoError(t, err)
	})
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
	// PUT's full-replace semantics all the way down to the database.
	updated, err := q.UpsertRestaurantProfile(ctx, storage.UpsertRestaurantProfileParams{
		Name:             "TEST-SENTINEL Trattoria Bellavista Renamed",
		Address:          "456 Side St",
		Phone:            "",
		Email:            "",
		Description:      "",
		PhotoData:        nil,
		PhotoContentType: pgtype.Text{Valid: false},
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

func TestRestaurantProfile_PhotoDataAndContentTypeMustBeSetTogether(t *testing.T) {
	// Proves the restaurant_profile_photo_pair CHECK constraint from
	// migration 000011 is real, not just a comment: a raw INSERT with one
	// half of the pair set and the other NULL must be rejected by the
	// database itself.
	conn, _, ctx := connectOrSkip(t)

	t.Cleanup(func() {
		_, err := conn.Exec(ctx, "DELETE FROM restaurant_profile WHERE id = 1")
		require.NoError(t, err)
	})
	_, err := conn.Exec(ctx, "DELETE FROM restaurant_profile WHERE id = 1")
	require.NoError(t, err)

	_, err = conn.Exec(ctx, `
		INSERT INTO restaurant_profile (id, name, photo_data, photo_content_type)
		VALUES (1, 'TEST-SENTINEL Half Photo', '\x89504e47', NULL)
	`)
	require.Error(t, err, "photo_data set without photo_content_type must violate the CHECK constraint")
	require.Contains(t, err.Error(), "check", "want a constraint violation, got: %v", err)
}
