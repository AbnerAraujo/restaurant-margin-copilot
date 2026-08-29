-- name: UpsertRestaurantProfile :one
-- Writer: internal/httpapi's profile handler only (PUT /api/profile).
-- restaurant_profile is pinned to exactly one row (id=1, see migration
-- 000011) — this is a full replace of that one row, not a partial patch,
-- so the caller must pass every field's intended final value, including an
-- unchanged photo it read back from GET /api/profile.
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
RETURNING *;

-- name: GetRestaurantProfile :one
-- Backs GET /api/profile. Returns pgx.ErrNoRows when the owner has not
-- saved a profile yet (id=1 never inserted) — the handler renders that as
-- an empty, not-yet-configured profile rather than an error.
SELECT * FROM restaurant_profile WHERE id = 1;
