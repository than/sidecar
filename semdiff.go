// semdiff.go — one line per item change between two board versions.
package main

import (
	"fmt"
	"strings"
	"unicode"
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

	// Pass 1: exact key matches.
	for _, n := range news {
		for _, o := range byKey[n.item.Key] {
			if o.matched {
				continue
			}
			o.matched, n.matched = true, true
			switch {
			case o.section != n.section:
				moved = append(moved, fmt.Sprintf("moved %s→%s: %q", sectionTag(o.section), sectionTag(n.section), title(n.item.Key)))
			case o.item.Raw != n.item.Raw:
				edited = append(edited, fmt.Sprintf("edited %s: %q", sectionTag(n.section), title(n.item.Key)))
			}
			break
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
// whole label. "Starts with an emoji" ≈ first field's first rune is a symbol.
func sectionTag(label string) string {
	fields := strings.Fields(label)
	if len(fields) > 1 {
		r := []rune(fields[0])[0]
		if unicode.IsSymbol(r) || r > 0x2600 {
			return fields[0]
		}
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
	o := strings.Split(strings.TrimSuffix(oldRaw, "\n"), "\n")
	n := strings.Split(strings.TrimSuffix(newRaw, "\n"), "\n")

	// LCS table, same shape as changedLines in diff.go.
	lcs := make([][]int32, len(o)+1)
	for i := range lcs {
		lcs[i] = make([]int32, len(n)+1)
	}
	for i := len(o) - 1; i >= 0; i-- {
		for j := len(n) - 1; j >= 0; j-- {
			if o[i] == n[j] {
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
	for i < len(o) || j < len(n) {
		switch {
		case i < len(o) && j < len(n) && o[i] == n[j]:
			i++
			j++
		case j < len(n) && (i == len(o) || lcs[i][j+1] >= lcs[i+1][j]):
			ops = append(ops, op{'+', n[j], i, j + 1})
			j++
		default:
			ops = append(ops, op{'-', o[i], i + 1, j})
			i++
		}
	}

	// Group consecutive ops into hunks and render headers.
	var out []string
	for k := 0; k < len(ops); {
		start := k
		for k+1 < len(ops) {
			cur, next := ops[k], ops[k+1]
			adjacent := (next.oi <= cur.oi+1) && (next.ni <= cur.ni+1)
			if !adjacent {
				break
			}
			k++
		}
		k++
		hunk := ops[start:k]
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
		if !haveO {
			firstO = hunk[0].oi
		}
		if !haveN {
			firstN = hunk[0].ni
		}
		out = append(out, hunkHeader(firstO, len(dels), firstN, len(adds)))
		out = append(out, dels...)
		out = append(out, adds...)
	}
	return out
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
