// diff.go
package main

import (
	"fmt"
	"regexp"
	"strings"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// stripANSI removes SGR escape sequences and OSC 8 hyperlink wrappers,
// leaving the visible text. Hyperlink targets (see linkify.go) never show
// up as text, matching how a terminal renders them.
func stripANSI(s string) string {
	return osc8RE.ReplaceAllString(ansiRE.ReplaceAllString(s, ""), "")
}

// changedLines returns the indices into newLines that are new or modified
// relative to oldLines — the lines not covered by the longest common
// subsequence of the two, compared on ANSI-stripped visible text. An inserted
// line marks only itself, not the identical lines shifted below it.
func changedLines(oldLines, newLines []string) map[int]bool {
	o := make([]string, len(oldLines))
	for i, l := range oldLines {
		o[i] = stripANSI(l)
	}
	n := make([]string, len(newLines))
	for i, l := range newLines {
		n[i] = stripANSI(l)
	}

	changed := map[int]bool{}

	// Trim the common prefix and suffix — unchanged lines need no diffing and
	// keep the DP table proportional to the edited region, not the whole file.
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
	// cells, skip line marking entirely rather than allocate hundreds of MB
	// per render. Real queues are far smaller; this only trips on huge docs.
	const maxDiffCells = 1 << 20 // ~4 MB with int32 cells; ~1000 lines a side
	if len(om)*len(nm) > maxDiffCells {
		return changed // empty — no markers, but the render still happens
	}

	// LCS length table over the differing middle. int32 halves the per-cell
	// footprint vs int — LCS lengths are bounded by the line count, far under
	// int32 max.
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

	// Walk; lines in the middle of nm not on the common subsequence are changed.
	i, j := 0, 0
	for j < len(nm) {
		if i < len(om) && om[i] == nm[j] {
			i++
			j++
		} else if i < len(om) && lcs[i+1][j] >= lcs[i][j+1] {
			i++
		} else {
			changed[start+j] = true
			j++
		}
	}
	return changed
}

// composeMarked renders the display string: changed bullet lines get their
// "• " swapped for a bright "▸ ", and (when flash is true) every changed line
// gets a subtle background tint. Order matters — the bullet is swapped first so
// applyLineBg re-establishes the background after the reset the swap introduces.
func composeMarked(lines []string, changed map[int]bool, flash bool, width int) string {
	ur, ug, ub := hexToRGB(colorUpdated)
	tr, tg, tb := hexToRGB(colorText)
	updatedMark := fmt.Sprintf("\x1b[1;38;2;%d;%d;%dm▸ \x1b[22;38;2;%d;%d;%dm", ur, ug, ub, tr, tg, tb)

	// owner[i] = index of the bullet line that owns line i (itself if it is a
	// bullet; the nearest preceding bullet within the same block otherwise), or
	// -1 if none. Single O(n) pass, reset at blank lines so ownership can't
	// cross a block boundary.
	owner := make([]int, len(lines))
	cur := -1
	for i, ln := range lines {
		if visibleWidth(ln) == 0 {
			cur = -1
		} else if isBulletLine(ln) {
			cur = i
		}
		owner[i] = cur
	}

	swap := map[int]bool{}
	for i := range lines {
		if changed[i] && owner[i] >= 0 {
			swap[owner[i]] = true
		}
	}

	out := make([]string, len(lines))
	for i, ln := range lines {
		if swap[i] {
			ln = strings.Replace(ln, "• ", updatedMark, 1)
		}
		if (changed[i] || swap[i]) && flash && visibleWidth(ln) > 0 {
			ln = applyLineBg(ln, colorFlashLineBg, width)
		}
		out[i] = ln
	}
	return strings.Join(out, "\n")
}

// isBulletLine reports whether the line's first visible content is glamour's
// "• " item prefix (so a "•" inside body text isn't matched).
func isBulletLine(ln string) bool {
	t := strings.TrimLeft(stripANSI(ln), " ")
	return strings.HasPrefix(t, "• ")
}

// applyLineBg tints the whole visible line with the given hex background,
// re-applying it after each SGR reset (a reset would otherwise clear the
// background mid-line), and pads to width so the tint spans the pane.
// It re-applies the background after each standalone ESC[0m reset; this relies
// on termenv emitting resets as a bare ESC[0m (a combined ESC[0;…m would drop
// the tint mid-line — not produced by the current renderer).
func applyLineBg(ln, hex string, width int) string {
	r, g, b := hexToRGB(hex)
	bg := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
	const reset = "\x1b[0m"
	body := strings.ReplaceAll(ln, reset, reset+bg)
	pad := ""
	if v := visibleWidth(ln); v < width {
		pad = strings.Repeat(" ", width-v)
	}
	return bg + body + pad + reset
}

// hexToRGB parses "#RRGGBB" into its components. Input is expected to be a
// valid hex color const; on a malformed value it falls back to a visible
// mid-grey rather than silently yielding black.
func hexToRGB(hex string) (int, int, int) {
	hex = strings.TrimPrefix(hex, "#")
	var r, g, b int
	if n, err := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b); n != 3 || err != nil {
		return 128, 128, 128
	}
	return r, g, b
}
