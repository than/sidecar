// diff.go
package main

import (
	"regexp"
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
