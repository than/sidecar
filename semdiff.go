// semdiff.go — one line per item change between two board versions.
package main

import (
	"fmt"
	"strings"
)

type itemRef struct {
	section string
	item    BoardItem
	matched bool
}

// semanticDiff reports item-level changes from old to new, one line each:
// moved (highest signal), edited, added, removed. Empty when no item changed
// — a caller seeing raw bytes differ but no output should fall back to a
// textual diff.
func semanticDiff(old, new Board) []string {
	olds := flatten(old)
	news := flatten(new)

	byKey := map[string][]*itemRef{}
	for _, r := range olds {
		byKey[r.item.Key] = append(byKey[r.item.Key], r)
	}

	var moved, edited, added, removed []string

	// Pass 1: exact key matches — same-section candidates first, so a key
	// duplicated across sections (e.g. a shared placeholder like "nothing
	// yet") never fabricates a moved line when an unmatched same-section
	// candidate is sitting right there. Cross-section is the fallback, used
	// only when no same-section candidate remains.
	for _, n := range news {
		var match *itemRef
		for _, o := range byKey[n.item.Key] {
			if !o.matched && o.section == n.section {
				match = o
				break
			}
		}
		if match == nil {
			for _, o := range byKey[n.item.Key] {
				if !o.matched {
					match = o
					break
				}
			}
		}
		if match == nil {
			continue
		}
		o := match
		o.matched, n.matched = true, true
		// Compare tags, not full labels: renaming a section's heading text
		// (e.g. "🚧 In progress" → "🚧 Working") keeps every item's tag the
		// same, so it must not report every item as "moved" to itself.
		ot, nt := sectionTag(o.section), sectionTag(n.section)
		switch {
		case ot != nt:
			moved = append(moved, fmt.Sprintf("moved %s→%s: %q", ot, nt, title(n.item.Key)))
		case o.item.Raw != n.item.Raw:
			edited = append(edited, fmt.Sprintf("edited %s: %q", nt, title(n.item.Key)))
		}
	}

	// Pass 2: prefix matches within the same section.
	for _, n := range news {
		if n.matched {
			continue
		}
		for _, o := range olds {
			if o.matched || o.section != n.section || !prefixMatch(o.item.Key, n.item.Key) {
				continue
			}
			o.matched, n.matched = true, true
			edited = append(edited, fmt.Sprintf("edited %s: %q", sectionTag(n.section), title(n.item.Key)))
			break
		}
	}

	for _, n := range news {
		if !n.matched {
			added = append(added, fmt.Sprintf("added %s: %q", sectionTag(n.section), title(n.item.Key)))
		}
	}
	for _, o := range olds {
		if !o.matched {
			removed = append(removed, fmt.Sprintf("removed %s: %q", sectionTag(o.section), title(o.item.Key)))
		}
	}

	out := append(moved, edited...)
	out = append(out, added...)
	return append(out, removed...)
}

func flatten(b Board) []*itemRef {
	var refs []*itemRef
	for _, s := range b.Sections {
		for _, it := range s.Items {
			refs = append(refs, &itemRef{section: s.Label, item: it})
		}
	}
	return refs
}

// sectionTag is the section's emoji when the label starts with one, else the
// whole label.
func sectionTag(label string) string {
	if emoji, ok := leadingEmoji(label); ok {
		return emoji
	}
	return label
}

// prefixMatch reports whether two keys share enough of a prefix to be the
// same item after an edit: at least 8 runes and at least half of the shorter.
func prefixMatch(a, b string) bool {
	ar, br := []rune(a), []rune(b)
	short := len(ar)
	if len(br) < short {
		short = len(br)
	}
	p := 0
	for p < short && ar[p] == br[p] {
		p++
	}
	return p >= 8 && p*2 >= short
}

// title clamps a key to 60 runes for the output line.
func title(key string) string {
	r := []rune(key)
	if len(r) <= 60 {
		return key
	}
	return string(r[:59]) + "…"
}

// diffLines is the diff the hook prints: semantic when both versions parse
// into sections and at least one item changed; a zero-context unified diff
// otherwise. Callers only invoke it when the raw bytes differ.
func diffLines(oldRaw, newRaw string) []string {
	ob, ok1 := parseBoard(oldRaw)
	nb, ok2 := parseBoard(newRaw)
	if ok1 && ok2 {
		if out := semanticDiff(ob, nb); len(out) > 0 {
			return out
		}
	}
	return unifiedU0(oldRaw, newRaw)
}

// unifiedU0 is a minimal unified diff with zero context lines: "@@" hunk
// headers plus -/+ lines only. Computed in-process — no shelling out.
func unifiedU0(oldRaw, newRaw string) []string {
	o := splitLines(oldRaw)
	n := splitLines(newRaw)

	// Trim the common prefix and suffix — same rationale as changedLines in
	// diff.go: unchanged lines need no diffing and keep the DP table
	// proportional to the edited region, not the whole file. start is the
	// offset added back into every line number below so headers stay
	// absolute.
	start := 0
	for start < len(o) && start < len(n) && o[start] == n[start] {
		start++
	}
	endO, endN := len(o), len(n)
	for endO > start && endN > start && o[endO-1] == n[endN-1] {
		endO--
		endN--
	}
	om, nm := o[start:endO], n[start:endN]

	// Guard against a pathological table on very large files: past this many
	// cells, skip the diff entirely rather than allocate hundreds of MB.
	// diffLines only calls unifiedU0 when the bytes differ, so this must
	// still surface something — a single explanatory line.
	const maxDiffCells = 1 << 20 // ~4 MB with int32 cells; ~1000 lines a side
	if len(om)*len(nm) > maxDiffCells {
		return []string{"file changed — too large to diff"}
	}

	// LCS table, same shape as changedLines in diff.go, over the trimmed middle.
	lcs := make([][]int32, len(om)+1)
	for i := range lcs {
		lcs[i] = make([]int32, len(nm)+1)
	}
	for i := len(om) - 1; i >= 0; i-- {
		for j := len(nm) - 1; j >= 0; j-- {
			if om[i] == nm[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	type op struct {
		kind byte // '-' or '+'
		text string
		oi   int // 1-based old line for '-', old position for '+'
		ni   int // 1-based new line
	}
	var ops []op
	i, j := 0, 0
	for i < len(om) || j < len(nm) {
		switch {
		case i < len(om) && j < len(nm) && om[i] == nm[j]:
			i++
			j++
		case j < len(nm) && (i == len(om) || lcs[i][j+1] >= lcs[i+1][j]):
			ops = append(ops, op{'+', nm[j], start + i, start + j + 1})
			j++
		default:
			ops = append(ops, op{'-', om[i], start + i + 1, start + j})
			i++
		}
	}

	// Group consecutive ops into hunks and render headers.
	var out []string
	for k := 0; k < len(ops); {
		h0 := k
		for k+1 < len(ops) {
			cur, next := ops[k], ops[k+1]
			adjacent := (next.oi <= cur.oi+1) && (next.ni <= cur.ni+1)
			if !adjacent {
				break
			}
			k++
		}
		k++
		hunk := ops[h0:k]
		var dels, adds []string
		firstO, firstN := 0, 0
		haveO, haveN := false, false
		for _, opv := range hunk {
			if opv.kind == '-' {
				dels = append(dels, "-"+opv.text)
				if !haveO {
					firstO, haveO = opv.oi, true
				}
			} else {
				adds = append(adds, "+"+opv.text)
				if !haveN {
					firstN, haveN = opv.ni, true
				}
			}
		}
		// A hunk with no dels is a pure insert: its old-side anchor is the
		// insert op's oi. A hunk with no adds is a pure delete: its new-side
		// anchor is the delete op's ni. GNU diff -U0 anchors headers the
		// same way, and a replace hunk's "-" side must report the deleted
		// line's actual old position, not wherever the tie-broken insert/
		// delete ops happened to land first in generation order.
		// anchorIdx is the 0-based count of old lines strictly before the
		// hunk, captured before the zero→1 display adjustment below so the
		// heading lookup (R6) sees the true position rather than the
		// display-only "line 1" fallback.
		var anchorIdx int
		if haveO {
			anchorIdx = firstO - 1
		} else {
			firstO = hunk[0].oi
			anchorIdx = firstO
			// A zero anchor means "before line 1" only when the old side is
			// genuinely empty. A non-empty old file whose insert lands at
			// the very start still anchors at line 1, matching GNU diff
			// -U0 (e.g. "c\n" → "x\nc\n" is "@@ -1,0 +1 @@", not "-0,0").
			if firstO == 0 && len(o) > 0 {
				firstO = 1
			}
		}
		if !haveN {
			firstN = hunk[0].ni
			if firstN == 0 && len(n) > 0 {
				firstN = 1
			}
		}
		header := hunkHeader(firstO, len(dels), firstN, len(adds))
		if heading := headingBefore(o, anchorIdx); heading != "" {
			header += " " + heading
		}
		out = append(out, header)
		out = append(out, dels...)
		out = append(out, adds...)
	}

	// A trailing-newline-only difference (e.g. "a\nb" vs "a\nb\n") produces
	// identical line slices, so the loop above emits no hunks even though
	// the raw bytes differ. diffLines only calls unifiedU0 when the bytes
	// differ, so returning nothing here would silently drop the change.
	// Surface it as a change to the final line; exact GNU "\ No newline at
	// end of file" annotations aren't required, just a non-empty diff.
	if len(out) == 0 && oldRaw != newRaw {
		last := ""
		if len(o) > 0 {
			last = o[len(o)-1]
		} else if len(n) > 0 {
			last = n[len(n)-1]
		}
		pos := len(o)
		if pos == 0 {
			pos = 1
		}
		out = append(out, hunkHeader(pos, 1, pos, 1), "-"+last, "+"+last)
	}
	return out
}

// headingBefore returns the last line in o[:limit] that starts with "#" — the
// nearest markdown heading preceding a hunk's position in the old file, like
// `diff -F '^#'`. Empty when no such line exists.
func headingBefore(o []string, limit int) string {
	if limit > len(o) {
		limit = len(o)
	}
	last := ""
	for idx := 0; idx < limit; idx++ {
		if strings.HasPrefix(o[idx], "#") {
			last = o[idx]
		}
	}
	return last
}

// splitLines splits raw text into lines without a trailing empty element for
// a trailing "\n", and returns an empty slice (not [""]) for empty input —
// strings.Split("", "\n") would otherwise yield a single phantom empty line.
func splitLines(raw string) []string {
	if raw == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(raw, "\n"), "\n")
}

// hunkHeader renders "@@ -a[,b] +c[,d] @@" in -U0 form: the count is omitted
// when it is 1, and a zero-count side keeps the position of the line before.
func hunkHeader(o, dels, n, adds int) string {
	side := func(pos, count int) string {
		if count == 0 {
			return fmt.Sprintf("%d,0", pos)
		}
		if count == 1 {
			return fmt.Sprintf("%d", pos)
		}
		return fmt.Sprintf("%d,%d", pos, count)
	}
	return fmt.Sprintf("@@ -%s +%s @@", side(o, dels), side(n, adds))
}
