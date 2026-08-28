package livedata

// Real filesystem tests against the actual backend/fixtures/ and
// backend/data/live/ directories (no mocking the filesystem) — the whole
// point of this package is a real, hardcoded path guarantee, which a faked
// filesystem couldn't prove.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDirAndFixturesDirAreAnchoredUnderBackend proves Dir/FixturesDir
// resolve to real, existing locations under backend/ regardless of the test
// runner's current working directory — the whole reason this package
// anchors to runtime.Caller(0) instead of os.Getwd() (spec FR-011).
func TestDirAndFixturesDirAreAnchoredUnderBackend(t *testing.T) {
	require.True(t, strings.HasSuffix(backendDir, "backend"), "backendDir should resolve to .../backend, got %q", backendDir)
	require.Equal(t, filepath.Join(backendDir, "data", "live"), Dir)
	require.Equal(t, filepath.Join(backendDir, "fixtures"), FixturesDir)

	info, err := os.Stat(FixturesDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())

	_, err = os.Stat(filepath.Join(FixturesDir, "supplier_cost_sheet.csv"))
	require.NoError(t, err, "the real, checked-in fixture must exist at the resolved FixturesDir")
}

// TestEnsureSeededNeverModifiesFixtures is this feature's single most
// important guarantee (Constitution: "never let this feature touch
// backend/fixtures/"), verified by an actual checksum comparison
// (spec SC-003), not just code inspection: hash every fixture file before
// and after calling EnsureSeeded (possibly multiple times, since it must be
// idempotent), and require byte-for-byte equality.
func TestEnsureSeededNeverModifiesFixtures(t *testing.T) {
	before, err := hashDirCSVs(FixturesDir)
	require.NoError(t, err)
	require.NotEmpty(t, before, "sanity check: FixturesDir must contain at least one CSV to hash")

	require.NoError(t, EnsureSeeded())
	require.NoError(t, EnsureSeeded()) // idempotency: calling twice must not error or re-copy destructively

	after, err := hashDirCSVs(FixturesDir)
	require.NoError(t, err)
	require.Equal(t, before, after, "backend/fixtures/ must be byte-for-byte unchanged after EnsureSeeded")
}

// TestEnsureSeededCopiesEveryFixtureCSV proves the seed actually landed —
// Dir contains the same set of CSVs FixturesDir does, with identical content.
func TestEnsureSeededCopiesEveryFixtureCSV(t *testing.T) {
	require.NoError(t, EnsureSeeded())

	fixtureHashes, err := hashDirCSVs(FixturesDir)
	require.NoError(t, err)

	for name, fixtureHash := range fixtureHashes {
		liveContent, err := os.ReadFile(filepath.Join(Dir, name))
		require.NoErrorf(t, err, "expected %s to have been seeded into %s", name, Dir)
		require.Equal(t, fixtureHash, sha256Hex(liveContent), "%s content must match its fixture source exactly", name)
	}
}

func hashDirCSVs(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string)
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".csv") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out[e.Name()] = sha256Hex(content)
	}
	return out, nil
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
