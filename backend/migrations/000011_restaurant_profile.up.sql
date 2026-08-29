-- Restaurant profile: the owner-entered company information and photo
-- shown on the new Profile page (backend/internal/httpapi/profile.go).
--
-- This is a SINGLE-tenant app (see specs/005-multi-tenant/spec.md: multi-
-- location/multi-restaurant support is deliberately out of scope and not
-- yet reviewed/approved) — there is exactly one restaurant's worth of data
-- anywhere in this database, and this table's whole point is to hold that
-- one restaurant's own company information. Rather than trust application
-- code alone to keep it to one row, the primary key is pinned to the
-- literal value 1 (the standard "singleton row" pattern): any second
-- INSERT collides on the primary key and fails loudly instead of silently
-- creating a second, ambiguous "current" profile. This is the zero-cost
-- forward-compatible choice the take-home brief asked for: it does not
-- build multi-tenancy, but it also does not paint into a corner — a real
-- multi-tenant migration would drop the id=1 CHECK, add a tenant_id column,
-- and make (tenant_id) the key instead, with no other schema change to this
-- table's fields.
--
-- The photo is stored as raw bytes in Postgres (bytea), not a data-URI
-- text column and not any external object store — this project has no
-- cloud-storage dependency anywhere and a single small profile photo does
-- not justify adding one. photo_content_type travels alongside it so the
-- API can hand the browser back a ready-to-render data URI
-- ("data:<content_type>;base64,<...>") without guessing the image format.
-- Both columns are NULL together (no photo uploaded yet) or NOT NULL
-- together (a photo was uploaded) — enforced by the CHECK constraint below,
-- never left in a half-set state.
--
-- Size enforcement (a "few MB" ceiling, per the take-home brief) happens in
-- internal/httpapi/profile.go BEFORE the INSERT/UPDATE ever runs — this
-- schema intentionally does not duplicate that cap as a CHECK on
-- octet_length(photo_data), since the authoritative reject-with-a-clear-
-- error path belongs at the API boundary (matching every other input
-- validation in this codebase — see promotions_create.go), not silently at
-- the database.

CREATE TABLE restaurant_profile (
    id                  SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    name                TEXT NOT NULL,
    address             TEXT NOT NULL DEFAULT '',
    phone               TEXT NOT NULL DEFAULT '',
    email               TEXT NOT NULL DEFAULT '',
    description         TEXT NOT NULL DEFAULT '',
    photo_data          BYTEA,
    photo_content_type  TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT restaurant_profile_photo_pair CHECK (
        (photo_data IS NULL) = (photo_content_type IS NULL)
    )
);

COMMENT ON TABLE restaurant_profile IS
    'The one restaurant this single-tenant prototype belongs to: its '
    'company information and photo, shown on and edited from the Profile '
    'page. Pinned to exactly one row (id=1) by design — see this '
    'migration''s file header for the forward-compatible rationale.';
