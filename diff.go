// diff.go
package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// stripANSI removes SGR escape sequences, leaving the visible text.
func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
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

	// LCS length table.
	lcs := make([][]int, len(o)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(n)+1)
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

	// Walk the table; lines in n that aren't part of the common subsequence
	// are the changed ones.
	changed := map[int]bool{}
	i, j := 0, 0
	for j < len(n) {
		if i < len(o) && o[i] == n[j] {
			i++
			j++
		} else if i < len(o) && lcs[i+1][j] >= lcs[i][j+1] {
			i++ // a line from old was removed
		} else {
			changed[j] = true // n[j] is new/modified
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
	updatedMark := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorUpdated)).Bold(true).Render("▸ ")

	out := make([]string, len(lines))
	for i, ln := range lines {
		if changed[i] {
			if isBulletLine(ln) {
				ln = strings.Replace(ln, "• ", updatedMark, 1)
			}
			if flash {
				ln = applyLineBg(ln, colorFlashLineBg, width)
			}
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

// hexToRGB parses "#RRGGBB" into its components.
func hexToRGB(hex string) (int, int, int) {
	hex = strings.TrimPrefix(hex, "#")
	var r, g, b int
	fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	return r, g, b
}
