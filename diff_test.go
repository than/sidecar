// diff_test.go
package main

import (
	"strings"
	"testing"
)

func idx(m map[int]bool) []int {
	out := []int{}
	for i := 0; i < 100; i++ {
		if m[i] {
			out = append(out, i)
		}
	}
	return out
}

func eq(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestChangedLinesIdentical(t *testing.T) {
	l := []string{"a", "b", "c"}
	if got := idx(changedLines(l, l)); len(got) != 0 {
		t.Errorf("identical → %v, want none", got)
	}
}

func TestChangedLinesOneModified(t *testing.T) {
	old := []string{"a", "b", "c"}
	nw := []string{"a", "B", "c"}
	if got := idx(changedLines(old, nw)); !eq(got, []int{1}) {
		t.Errorf("modified → %v, want [1]", got)
	}
}

func TestChangedLinesInsertionNoCascade(t *testing.T) {
	// Inserting one line must mark ONLY the new line, not everything below.
	old := []string{"a", "b", "c"}
	nw := []string{"a", "NEW", "b", "c"}
	if got := idx(changedLines(old, nw)); !eq(got, []int{1}) {
		t.Errorf("insertion → %v, want [1] (no cascade)", got)
	}
}

func TestChangedLinesIgnoresANSI(t *testing.T) {
	old := []string{"\x1b[31mhello\x1b[0m"}
	nw := []string{"\x1b[32mhello\x1b[0m"} // same text, different color
	if got := idx(changedLines(old, nw)); len(got) != 0 {
		t.Errorf("restyle-only → %v, want none", got)
	}
}

func TestComposeMarkedBulletSwap(t *testing.T) {
	// A changed bullet line: the literal "• " becomes a styled "▸ ".
	lines := []string{"\x1b[38;2;208;208;208m• \x1b[0malpha"}
	out := composeMarked(lines, map[int]bool{0: true}, false, 40)
	if strings.Contains(stripANSI(out), "• ") {
		t.Errorf("bullet not replaced:\n%q", out)
	}
	if !strings.Contains(stripANSI(out), "▸ alpha") {
		t.Errorf("expected ▸ alpha, got:\n%q", stripANSI(out))
	}
}

func TestComposeMarkedUnchangedLineUntouched(t *testing.T) {
	lines := []string{"\x1b[38;2;208;208;208m• \x1b[0malpha"}
	out := composeMarked(lines, map[int]bool{}, false, 40) // nothing changed
	if out != lines[0] {
		t.Errorf("unchanged line altered:\n%q", out)
	}
}

func TestComposeMarkedNonBulletNoMarker(t *testing.T) {
	// A changed non-bullet line gets no ▸ (bullets only), and without flash
	// its text is unchanged.
	lines := []string{"\x1b[38;2;209;154;102m▍ Heading\x1b[0m"}
	out := composeMarked(lines, map[int]bool{0: true}, false, 40)
	if strings.Contains(out, "▸") {
		t.Errorf("non-bullet line should not get ▸:\n%q", out)
	}
}

func TestComposeMarkedFlashAddsBackground(t *testing.T) {
	lines := []string{"\x1b[38;2;208;208;208m• \x1b[0malpha"}
	out := composeMarked(lines, map[int]bool{0: true}, true, 40)
	if !strings.Contains(out, "\x1b[48;2;") {
		t.Errorf("flash should inject a background SGR:\n%q", out)
	}
	// The ▸ still shows through the flash.
	if !strings.Contains(stripANSI(out), "▸ alpha") {
		t.Errorf("▸ missing under flash:\n%q", stripANSI(out))
	}
}

func TestComposeMarkedNoFlashNoBackground(t *testing.T) {
	lines := []string{"\x1b[38;2;208;208;208m• \x1b[0malpha"}
	out := composeMarked(lines, map[int]bool{0: true}, false, 40)
	if strings.Contains(out, "\x1b[48;2;") {
		t.Errorf("no flash should not inject a background:\n%q", out)
	}
}

func TestComposeMarkedWrappedContinuationMarksOwningBullet(t *testing.T) {
	// A long bullet wrapped onto a continuation line; the edit landed on the
	// continuation (index 1). The ▸ must appear on the bullet line (index 0).
	lines := []string{
		"\x1b[38;2;208;208;208m• \x1b[0malpha the first",
		"\x1b[38;2;208;208;208m  and its wrapped tail\x1b[0m",
	}
	out := composeMarked(lines, map[int]bool{1: true}, false, 40)
	got := strings.Split(stripANSI(out), "\n")
	if !strings.Contains(got[0], "▸ ") {
		t.Errorf("owning bullet not marked:\n%q", got[0])
	}
	if strings.Contains(got[1], "▸") {
		t.Errorf("continuation line should not itself get ▸:\n%q", got[1])
	}
}

func TestComposeMarkedChangedProseAfterBlankNoMarker(t *testing.T) {
	// A changed non-bullet line preceded by a blank line: the backward walk
	// stops at the blank, so nothing is marked.
	lines := []string{
		"",
		"\x1b[38;2;208;208;208mjust prose\x1b[0m",
	}
	out := composeMarked(lines, map[int]bool{1: true}, false, 40)
	if strings.Contains(out, "▸") {
		t.Errorf("prose after a blank should get no marker:\n%q", stripANSI(out))
	}
}

func TestChangedLinesTrimmedContext(t *testing.T) {
	old := []string{"h", "a", "b", "c", "z"}
	nw := []string{"h", "a", "X", "c", "z"} // only index 2 changed
	if got := idx(changedLines(old, nw)); !eq(got, []int{2}) {
		t.Errorf("trimmed-context change → %v, want [2]", got)
	}
}

func TestChangedLinesDeletionNoSpuriousMark(t *testing.T) {
	// Deleting a line should not mark the surrounding context in the new render.
	old := []string{"a", "b", "c"}
	nw := []string{"a", "c"}
	if got := idx(changedLines(old, nw)); len(got) != 0 {
		t.Errorf("deletion → %v, want none (nothing added in new)", got)
	}
}

func TestChangedLinesCapDegradesToEmpty(t *testing.T) {
	// Two large, fully-different slices would blow the table; the cap must
	// return an empty set instead of allocating/panicking.
	n := 5000
	old := make([]string, n)
	nw := make([]string, n)
	for i := 0; i < n; i++ {
		old[i] = "old-" + string(rune('a'+i%26))
		nw[i] = "new-" + string(rune('a'+i%26))
	}
	// Force no common prefix/suffix so the trim can't shrink it.
	old[0], nw[0] = "A", "B"
	old[n-1], nw[n-1] = "Y", "Z"
	if got := len(changedLines(old, nw)); got != 0 {
		t.Errorf("oversized diff should mark nothing, got %d", got)
	}
}
