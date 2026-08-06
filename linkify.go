// linkify.go
package main

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// bareURLLineRE matches a raw markdown line that is, per the board's own
// convention, nothing but a bare URL — optionally under a "- "/"* "/"+ "
// bullet. Only these lines are candidates for the wrap fix: URLs embedded in
// running prose are left to glamour as before.
var bareURLLineRE = regexp.MustCompile(`^(\s*[-*+]\s+)?(https?://\S+)\s*$`)

// osc8Open opens an OSC 8 hyperlink; osc8Close ends it. BEL-terminated (not
// ST) — a single byte, and it sidesteps the fact that this codebase's ANSI
// helpers (reflow's wordwrap/width, and stripANSI/visibleWidth below) only
// recognize CSI sequences, so an ST terminator ("\x1b\\") would never be
// consumed as a unit.
func osc8Open(target string) string { return "\x1b]8;;" + target + "\x07" }

const osc8Close = "\x1b]8;;\x07"

// urlTruncation records a bare URL whose display text had to be shortened
// to fit the render width, and what it was shortened to.
type urlTruncation struct {
	display string // the ellipsized text that replaced the URL in raw markdown
	full    string // the original, untruncated URL
}

// truncateBareURLs rewrites raw markdown so that any bare-URL-only line
// wider than width is shortened to fit, with a trailing ellipsis. Lines that
// already fit are left untouched — they render (and wrap-test) exactly as
// before. Returns the rewritten markdown and the list of shortenings made,
// so the caller can re-attach the full URL as an OSC 8 target after
// rendering.
func truncateBareURLs(raw string, width int) (string, []urlTruncation) {
	lines := strings.Split(raw, "\n")
	var truncations []urlTruncation
	for i, line := range lines {
		m := bareURLLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		prefix, url := m[1], m[2]
		reserve := 0
		if prefix != "" {
			reserve = 2 // glamour renders any bullet marker as "• "
		}
		budget := width - reserve
		if budget < 8 {
			budget = 8
		}
		if len([]rune(url)) <= budget {
			continue // fits as-is; let glamour's existing autolink path handle it
		}
		keep := budget - 1 // room for the ellipsis
		if keep < 1 {
			keep = 1
		}
		display := string([]rune(url)[:keep]) + "…"
		lines[i] = prefix + display
		truncations = append(truncations, urlTruncation{display: display, full: url})
	}
	return strings.Join(lines, "\n"), truncations
}

// linkifyTruncations wraps each truncation's display text, wherever it
// landed in the rendered+tidied output, in a colorLink-styled OSC 8
// hyperlink pointing at the full URL. The visible text is unchanged (still
// the ellipsized form); only a terminal that understands OSC 8 sees the full
// target, so clicking anywhere on the truncated text still opens it.
func linkifyTruncations(rendered string, truncations []urlTruncation) string {
	for _, t := range truncations {
		start, end, ok := findPlainRange(rendered, t.display)
		if !ok {
			continue
		}
		r, g, b := hexToRGB(colorLink)
		styled := osc8Open(t.full) +
			"\x1b[4;38;2;" + strconv.Itoa(r) + ";" + strconv.Itoa(g) + ";" + strconv.Itoa(b) + "m" +
			t.display + "\x1b[0m" + osc8Close
		rendered = rendered[:start] + styled + rendered[end:]
	}
	return rendered
}

// findPlainRange locates the byte range within s — which may be laced with
// ANSI escape sequences — that renders the given plain substring. ANSI
// sequences (CSI "\x1b[...letter" and OSC "\x1b]...BEL" or "\x1b]...ST")
// are skipped when matching so `plain` is matched against visible text only;
// the returned range spans the underlying bytes exactly, escapes included,
// so it can be sliced out and replaced wholesale.
func findPlainRange(s, plain string) (start, end int, ok bool) {
	if plain == "" {
		return 0, 0, false
	}
	target := []rune(plain)

	var runeStart []int // byte offset in s where each collected plain rune begins
	var runeEnd []int   // byte offset in s just past that rune
	var plainRunes []rune

	i := 0
	for i < len(s) {
		if s[i] == 0x1b {
			j := skipANSISeq(s, i)
			i = j
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		plainRunes = append(plainRunes, r)
		runeStart = append(runeStart, i)
		runeEnd = append(runeEnd, i+size)
		i += size
	}

	idx := indexRunes(plainRunes, target)
	if idx < 0 {
		return 0, 0, false
	}
	start = runeStart[idx]
	end = runeEnd[idx+len(target)-1]
	return start, end, true
}

// skipANSISeq returns the index in s just past the ANSI escape sequence
// starting at i (where s[i] == ESC).
func skipANSISeq(s string, i int) int {
	j := i + 1
	if j >= len(s) {
		return j
	}
	switch s[j] {
	case '[': // CSI: ESC [ ... letter
		j++
		for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
			j++
		}
		if j < len(s) {
			j++
		}
	case ']': // OSC: ESC ] ... BEL, or ESC ] ... ESC \
		j++
		for j < len(s) {
			if s[j] == 0x07 {
				j++
				break
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				j += 2
				break
			}
			j++
		}
	default:
		j++
	}
	return j
}

// indexRunes finds the first index in haystack where needle occurs, or -1.
func indexRunes(haystack, needle []rune) int {
	if len(needle) == 0 || len(needle) > len(haystack) {
		if len(needle) == 0 {
			return 0
		}
		return -1
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j, r := range needle {
			if haystack[i+j] != r {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
