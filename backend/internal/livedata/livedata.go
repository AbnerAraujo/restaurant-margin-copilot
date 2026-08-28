// Package livedata is the single source of truth for the "live data"
// directory specs/007-cost-sheet-upload writes into: backend/data/live/, a
// git-ignored directory that starts as a copy of the checked-in
// backend/fixtures/ and is what real web-UI uploads modify going forward.
//
// Two things this package guarantees, both load-bearing for the
// Constitution's "never let this feature touch backend/fixtures/" rule:
//
//  1. Dir is a fixed, hardcoded location, resolved at package-init time from
//     THIS FILE's own compile-time location (runtime.Caller), never from
//     os.Getwd() or any request input. This project's dev server has
//     actually been started from different working directories across
//     sessions (repo root for some -ingest invocations per
//     specs/001-margin-reconciliation-qa/quickstart.md, backend/ for how
//     -serve has actually been launched elsewhere) — an os.Getwd()-relative
//     path would silently resolve to a different directory depending on
//     that accident of invocation. Anchoring to runtime.Caller(0) makes Dir
//     a true constant: the same absolute path every run, and one that no
//     request can ever influence because no request-derived string ever
//     participates in building it.
//  2. FixturesDir is read-only from this package's point of view — nothing
//     here ever writes to it. EnsureSeeded only ever copies FROM
//     FixturesDir TO Dir, never the reverse.
package livedata

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var (
	// backendDir is backend/ — this file's own directory
	// (backend/internal/livedata) two levels up — resolved once, at
	// package-init time, from the compiled-in source path rather than the
	// process's current working directory.
	backendDir = func() string {
		_, thisFile, _, ok := runtime.Caller(0)
		if !ok {
			// Unreachable in practice (runtime.Caller(0) always succeeds for
			// the calling frame itself), but fail loudly rather than silently
			// falling back to a guessed path if it ever were to happen —
			// Constitution Principle II applies to this package's own
			// bootstrapping, not just to parsed data.
			panic("livedata: runtime.Caller(0) failed to resolve this package's own source location")
		}
		return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	}()

	// Dir is the live-data directory real uploads modify: backend/data/live/.
	// Git-ignored (see .gitignore) — never the destination of a git commit.
	Dir = filepath.Join(backendDir, "data", "live")

	// FixturesDir is the checked-in, git-tracked reference dataset
	// (backend/fixtures/) that Dir is seeded from. This package only ever
	// reads from FixturesDir, never writes to it.
	FixturesDir = filepath.Join(backendDir, "fixtures")
)

// EnsureSeeded creates Dir and copies every *.csv file from FixturesDir into
// it, if and only if Dir does not already exist. Idempotent and safe to call
// on every request: an operator's own uploads, once committed, are never
// clobbered by a later call — this only ever runs once, on first use.
func EnsureSeeded() error {
	if _, err := os.Stat(Dir); err == nil {
		return nil // already seeded (or already has real uploaded data in it)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("livedata: checking %s: %w", Dir, err)
	}

	if err := os.MkdirAll(Dir, 0o755); err != nil {
		return fmt.Errorf("livedata: creating %s: %w", Dir, err)
	}

	entries, err := os.ReadDir(FixturesDir)
	if err != nil {
		return fmt.Errorf("livedata: reading seed source %s: %w", FixturesDir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".csv") {
			continue
		}
		if err := copyFile(filepath.Join(FixturesDir, e.Name()), filepath.Join(Dir, e.Name())); err != nil {
			return fmt.Errorf("livedata: seeding %s: %w", e.Name(), err)
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
