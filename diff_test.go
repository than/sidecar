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

func TestChangedLinesDeletionNoSpuriousMark(t *testing.T) {
	// Deleting a line should not mark the surrounding context in the new render.
	old := []string{"a", "b", "c"}
	nw := []string{"a", "c"}
	if got := idx(changedLines(old, nw)); len(got) != 0 {
		t.Errorf("deletion → %v, want none (nothing added in new)", got)
	}
}
