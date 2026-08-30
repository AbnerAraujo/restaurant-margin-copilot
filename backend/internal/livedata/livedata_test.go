package livedata

// Real filesystem tests against the actual backend/data/live/ directory (no
// mocking the filesystem) — the whole point of this package is a real,
// hardcoded path guarantee, which a faked filesystem couldn't prove.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDirIsAnchoredUnderBackend proves Dir resolves to the real location
// under backend/ regardless of the test runner's current working directory
// — the whole reason this package anchors to runtime.Caller(0) instead of
// os.Getwd() (spec FR-011).
func TestDirIsAnchoredUnderBackend(t *testing.T) {
	require.True(t, strings.HasSuffix(backendDir, "backend"), "backendDir should resolve to .../backend, got %q", backendDir)
	require.Equal(t, filepath.Join(backendDir, "data", "live"), Dir)
}

// TestEnsureReadyAgainstRealDataset: on a machine where the dataset has
// been generated (cmd/gendata), EnsureReady must succeed and must never
// modify the directory (it is a pure check, not a bootstrap — see its doc
// comment for why refusing beats auto-seeding here). On a fresh clone with
// no dataset yet, it must refuse with an error naming the gendata command,
// exercised below by moving the real directory aside.
func TestEnsureReadyAgainstRealDataset(t *testing.T) {
	if _, err := os.Stat(Dir); os.IsNotExist(err) {
		t.Skipf("no generated dataset at %s on this machine; the fresh-clone refusal path is covered by TestEnsureReadyRefusesWhenDatasetMissing", Dir)
	}

	require.NoError(t, EnsureReady())
}

// TestEnsureReadyRefusesWhenDatasetMissing proves the fresh-clone path:
// with no dataset directory at all, EnsureReady returns an error that
// tells the operator exactly how to generate one, rather than silently
// creating an empty directory an upload could then re-ingest as if it
// were the whole dataset.
func TestEnsureReadyRefusesWhenDatasetMissing(t *testing.T) {
	withMissingDir(t)

	err := EnsureReady()
	require.Error(t, err)
	require.Contains(t, err.Error(), "cmd/gendata", "the refusal must name the command that fixes it")
}

// withMissingDir makes Dir genuinely not-exist for the duration of the
// calling test, then restores whatever was really there — real generated
// data on this machine, nothing on a fresh clone — via t.Cleanup,
// regardless of how the test finishes. Dir itself is fixed and
// non-injectable by this package's own design (never request- or
// environment-derived), so a test needing a missing Dir must move the real
// one aside rather than pointing EnsureReady at some other path.
func withMissingDir(t *testing.T) {
	t.Helper()

	if _, err := os.Stat(Dir); err != nil {
		if os.IsNotExist(err) {
			return // already missing — nothing to preserve or restore
		}
		require.NoError(t, err)
	}

	backup := Dir + ".test-backup"
	require.NoError(t, os.RemoveAll(backup))
	require.NoError(t, os.Rename(Dir, backup))

	t.Cleanup(func() {
		_ = os.RemoveAll(Dir)
		if err := os.Rename(backup, Dir); err != nil {
			t.Errorf("withMissingDir: FAILED to restore real content back to %s from %s: %v — check that path by hand", Dir, backup, err)
		}
	})
}
