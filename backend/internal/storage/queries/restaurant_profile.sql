-- name: UpsertRestaurantProfile :one
-- Writer: internal/httpapi's profile handler only (PUT /api/profile).
-- restaurant_profile is pinned to exactly one row (id=1, see migration
-- 000011) — this is a full replace of that one row, not a partial patch,
-- so the caller must pass every field's intended final value, including an
-- unchanged photo it read back from GET /api/profile.
--
-- Optimistic concurrency (QA finding, lost-update fix): ExpectedUpdatedAt
-- must equal the row's CURRENT updated_at for the ON CONFLICT branch to take
-- effect. A caller passes back exactly the updated_at it last read from
-- GET/PUT /api/profile; if someone else has saved in between, the row's
-- updated_at has moved on and this WHERE clause is false, so the DO UPDATE
-- is skipped entirely and RETURNING yields zero rows — surfaced to Go as
-- pgx.ErrNoRows, which profile.go maps to 409 Conflict rather than silently
-- overwriting the newer save. A caller with no profile loaded yet passes an
-- invalid/NULL ExpectedUpdatedAt: on a genuinely empty table the INSERT
-- branch runs unconditionally (no row to conflict with, so no check is
-- needed), and if a row already exists by then, NULL never equals a real
-- timestamp, so this correctly refuses as a conflict too.
INSERT INTO restaurant_profile (
    id, name, address, phone, email, description, photo_data, photo_content_type
) VALUES (
    1, $1, $2, $3, $4, $5, $6, $7
)
ON CONFLICT (id) DO UPDATE SET
    name               = EXCLUDED.name,
    address            = EXCLUDED.address,
    phone              = EXCLUDED.phone,
    email              = EXCLUDED.email,
    description        = EXCLUDED.description,
    photo_data         = EXCLUDED.photo_data,
    photo_content_type = EXCLUDED.photo_content_type,
    updated_at         = now()
WHERE restaurant_profile.updated_at = sqlc.arg(expected_updated_at)
RETURNING *;

-- name: GetRestaurantProfile :one
-- Backs GET /api/profile. Returns pgx.ErrNoRows when the owner has not
-- saved a profile yet (id=1 never inserted) — the handler renders that as
-- an empty, not-yet-configured profile rather than an error.
SELECT * FROM restaurant_profile WHERE id = 1;
