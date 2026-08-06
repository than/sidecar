// collapse.go — rendering-only section collapse: header item counts and
// stripping a collapsed section's body out of the markdown before it
// reaches glamour. The board file on disk is never touched.
package main

import (
	"strconv"
	"strings"
)

// itemCount is a section's item count, excluding the "nothing yet"
// placeholder every scaffolded empty section starts with (sections.go).
func itemCount(s BoardSection) int {
	n := 0
	for _, it := range s.Items {
		if it.Key == emptySectionPlaceholder {
			continue
		}
		n++
	}
	return n
}

// seedDefaults seeds collapsed[label] the first time a label is seen —
// true only for the exact labels "✅ Done" and "📦 Shipped", false for
// everything else. A label already present in the map (the user has
// toggled it, or it was seeded on a prior reload) is left untouched, so
// this is safe to call on every reload.
func seedDefaults(board Board, collapsed map[string]bool) {
	for _, s := range board.Sections {
		if _, seen := collapsed[s.Label]; seen {
			continue
		}
		collapsed[s.Label] = s.Label == "✅ Done" || s.Label == "📦 Shipped"
	}
}

// applyCollapse rewrites raw markdown: every section heading gets its item
// count appended as "(N)" (N excludes the placeholder, shown even at 0),
// and every item belonging to a collapsed section is dropped entirely —
// its bullet line and every continuation line. board must be the result of
// parseBoard(raw); when it has no sections (parseBoard returned ok=false),
// raw is returned unchanged.
func applyCollapse(raw string, board Board, collapsed map[string]bool) string {
	if len(board.Sections) == 0 {
		return raw
	}
	lines := strings.Split(raw, "\n")

	drop := make([]bool, len(lines))
	counts := make(map[int]int, len(board.Sections)) // HeaderLine -> count
	for _, s := range board.Sections {
		counts[s.HeaderLine] = itemCount(s)
		if !collapsed[s.Label] {
			continue
		}
		for _, it := range s.Items {
			for i := it.StartLine; i <= it.EndLine && i < len(lines); i++ {
				drop[i] = true
			}
		}
	}

	var b strings.Builder
	for i, ln := range lines {
		if drop[i] {
			continue
		}
		if n, ok := counts[i]; ok {
			ln += " (" + strconv.Itoa(n) + ")"
		}
		b.WriteString(ln)
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}
