-- Versions the shape of the JSON blob stored in answer_cache.response.
--
-- answer_cache.response stores a full serialized httpapi.AskResponse.
-- That type has grown fields since this table was first created
-- (Visualization, Cache, MatchKind) and will grow more. Go's JSON decoder is
-- lenient: a pre-Visualization blob unmarshals successfully into today's
-- AskResponse with Visualization simply nil, which is INDISTINGUISHABLE from
-- a legitimately chart-less answer — a stale response shape served with
-- silent, wrong-looking confidence instead of failing loudly. The cache is
-- cleared on every ingestion run (see internal/answercache's package doc),
-- but not on a code deploy, so a stale shape can otherwise survive across
-- deploys indefinitely.
--
-- This is the backend analog of a bug frontend/src/lib/chatStorage.ts's
-- THREADS_VERSION already exists to prevent for browser-persisted chat
-- threads, whose ChatMessage shape has changed the same way.
--
-- schema_version is nullable and gets no default: every row written before
-- this migration has NULL here, and NULL is treated exactly like a version
-- mismatch by internal/answercache.Lookup — a cache miss, not a hit. New
-- writes always set it to answercache.CurrentSchemaVersion explicitly (see
-- cache.go), so a mismatch after a future AskResponse shape change is
-- detected the same way: invalidate rather than risk serving a stale shape.
ALTER TABLE answer_cache ADD COLUMN schema_version INTEGER;

COMMENT ON COLUMN answer_cache.schema_version IS
    'Version of the httpapi.AskResponse JSON shape stored in response, per '
    'answercache.CurrentSchemaVersion. NULL (pre-migration rows) or any '
    'value other than the current constant is treated as a cache miss on '
    'read, never served — the same discipline frontend/src/lib/chatStorage.ts '
    'applies to browser-persisted chat threads via THREADS_VERSION.';
