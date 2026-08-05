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
