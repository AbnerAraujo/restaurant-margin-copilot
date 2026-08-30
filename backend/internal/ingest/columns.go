package ingest

import (
	"fmt"
	"strings"
)

// headerIndex resolves logical field names to CSV column indices, tolerating
// realistic real-world header variance (case, spaces vs underscores vs
// hyphens, a trailing "number"/"#") rather than requiring the dataset
// files' exact column names. This is a real design constraint, not
// polish: research.md's real-file-compatibility decision requires ingestion
// to survive an actual restaurant/bar's own export files, whose column
// names will not match this project's own CSVs byte-for-byte.
type headerIndex struct {
	index map[string]int
}

func newHeaderIndex(header []string) headerIndex {
	idx := make(map[string]int, len(header))
	for i, h := range header {
		idx[normalizeHeader(h)] = i
	}
	return headerIndex{index: idx}
}

func normalizeHeader(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	h = strings.NewReplacer(" ", "_", "-", "_", "#", "_number", "%", "_pct").Replace(h)
	for strings.Contains(h, "__") {
		h = strings.ReplaceAll(h, "__", "_")
	}
	return strings.Trim(h, "_")
}

// find returns the column index for the first matching alias, or -1 if none
// of the aliases appear in the header.
func (h headerIndex) find(aliases ...string) int {
	for _, a := range aliases {
		if i, ok := h.index[normalizeHeader(a)]; ok {
			return i
		}
	}
	return -1
}

// require behaves like find but returns an error naming every alias tried,
// rather than silently returning -1 for the caller to mishandle — a missing
// required column is exactly the kind of "data is incomplete" situation
// Constitution Principle II says to refuse on, not guess through.
//
// No "ingest: " prefix here: every call site in ingest.go/promo.go
// immediately re-wraps this as "ingest: <file>: %w", so a prefix here would
// double up into "ingest: <file>: ingest: required column...".
func (h headerIndex) require(field string, aliases ...string) (int, error) {
	i := h.find(aliases...)
	if i < 0 {
		return -1, fmt.Errorf("required column %q not found (tried: %s)", field, strings.Join(aliases, ", "))
	}
	return i, nil
}

// get safely reads a field by column index, tolerating short rows (a
// trailing optional column simply omitted) rather than panicking.
func get(row []string, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}

func isBlankRow(row []string) bool {
	for _, f := range row {
		if strings.TrimSpace(f) != "" {
			return false
		}
	}
	return true
}
