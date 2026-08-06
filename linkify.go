// linkify.go
package main

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// bareURLLineRE matches a raw markdown line that is, per the board's own
// convention, nothing but a bare URL — optionally under a "- "/"* "/"+ "
// bullet. Only these lines are candidates for the wrap fix: URLs embedded in
// running prose are left to glamour as before.
//
// Deliberately unhandled shapes (out of scope for this fix, not oversights):
// an ordered-list URL ("1. https://…" — Enumeration uses a different
// glamour prefix/width than Item, and ordered bare-URL items don't appear in
// this board's own convention or fixture); a block-quoted URL ("> https://…"
// — BlockQuote's "│ " indent isn't accounted for by the reserve calc below);
// and a task-list URL ("- [ ] https://…" — the "[ ] "/"[x] " checkbox eats
// more width than a plain bullet, and isn't reflected in the reserve
// either). Each would need its own reserve term; none currently appear on
// real boards using this viewer. A URL in any of these shapes still renders
// — just with the pre-#15-fix wrapping behavior if it's too long, not the
// truncate+hyperlink treatment.
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

// fenceRE matches a fenced-code-block delimiter line (``` or ~~~, any
// fence-length/info-string suffix) and captures which character it's built
// from, so a run of ``` can't be closed early by an unrelated ~~~ line (or
// vice versa) — same rule CommonMark itself uses.
var fenceRE = regexp.MustCompile("^\\s*(`{3,}|~{3,})")

// osc8Safe reports whether url is safe to embed as the target of an OSC 8
// escape: every byte in the printable ASCII range 0x20–0x7E, matching what
// the OSC 8 spec itself requires of a URI. bareURLLineRE's "\S+" happily
// matches raw control bytes (BEL, ESC, ...) if the source markdown contains
// them — embedding one verbatim in "\x1b]8;;<url>\x07" would let it
// terminate the escape early (or start a new one) and inject arbitrary
// terminal control sequences into the render.
//
// This gates only the hyperlink attachment in linkifyTruncations, not the
// display-text truncation in truncateBareURLs: the truncated text is plain
// markdown, rendered exactly the way glamour already renders any bare URL's
// raw bytes today — no new escape sequence is built from it, so no new
// injection surface. Excluding it from truncation entirely would also have
// silently reintroduced the wrap bug (issue #15) for any URL containing a
// wide/non-ASCII rune (see the cell-width fix below), which is legitimate
// content, not an attack.
func osc8Safe(url string) bool {
	for i := 0; i < len(url); i++ {
		if url[i] < 0x20 || url[i] > 0x7e {
			return false
		}
	}
	return true
}

// truncateBareURLs rewrites raw markdown so that any bare-URL-only line
// wider than width is shortened to fit, with a trailing ellipsis. Lines that
// already fit, or that fall inside a fenced code block (verbatim content —
// truncating it would silently alter what the fence reproduces), are left
// untouched. skip holds full URLs to leave untruncated regardless of width —
// used by renderMarkdown to retry a line whose truncated form couldn't be
// relocated after rendering (see renderMarkdown for why).
//
// Returns the rewritten markdown and the list of shortenings made, so the
// caller can re-attach the full URL as an OSC 8 target after rendering.
func truncateBareURLs(raw string, width int, skip map[string]bool) (string, []urlTruncation) {
	lines := strings.Split(raw, "\n")
	var truncations []urlTruncation
	fenceChar := byte(0) // 0 when not inside a fence; '`' or '~' while inside one
	for i, line := range lines {
		if m := fenceRE.FindStringSubmatch(line); m != nil {
			c := m[1][0]
			if fenceChar == 0 {
				fenceChar = c
			} else if c == fenceChar {
				fenceChar = 0
			}
			// A fence line of the OTHER character, while already inside a
			// fence, is just content (e.g. a ~~~ example inside a ``` block)
			// — state doesn't change.
			continue
		}
		if fenceChar != 0 {
			continue
		}
		m := bareURLLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		prefix, url := m[1], m[2]
		if skip[url] {
			continue
		}
		// Reserve the columns glamour will spend on indent + bullet: any
		// leading whitespace in the source (one level of nesting per 2
		// cols, matching styleConfig's List.LevelIndent) plus "• " for the
		// marker itself. A plain, unbulleted line reserves nothing.
		reserve := 0
		if prefix != "" {
			leadingWS := len(prefix) - len(strings.TrimLeft(prefix, " \t"))
			reserve = leadingWS + 2
		}
		budget := width - reserve
		// Nesting can push reserve arbitrarily high (unlike the old
		// hard-coded reserve of 2), so this floor stays reachable: without
		// it, deeply nested lines could get a negative/zero budget.
		if budget < 8 {
			budget = 8
		}
		// Budget and cut are measured in terminal cells (visibleWidth /
		// go-runewidth), not runes: a rune-count budget measures a
		// wide-rune URL (CJK domains, box-drawing, ...) as shorter than it
		// actually renders, lets it through uncut, and glamour force-wraps
		// it anyway — falling back to exactly the broken behavior this fix
		// exists to prevent, just for non-ASCII URLs instead of long ones.
		if visibleWidth(url) <= budget {
			continue // fits as-is; let glamour's existing autolink path handle it
		}
		display := cutToCellWidth(url, budget-1) + "…" // -1: room for the ellipsis
		lines[i] = prefix + display
		truncations = append(truncations, urlTruncation{display: display, full: url})
	}
	return strings.Join(lines, "\n"), truncations
}

// cutToCellWidth returns the longest prefix of s whose total terminal cell
// width (go-runewidth — East-Asian-wide runes count as 2) does not exceed
// maxCells.
func cutToCellWidth(s string, maxCells int) string {
	if maxCells < 1 {
		maxCells = 1
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if w+rw > maxCells {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String()
}

// linkifyTruncations wraps each truncation's display text, wherever it
// landed in the rendered+tidied output, in a colorLink-styled OSC 8
// hyperlink pointing at the full URL. The visible text is unchanged (still
// the ellipsized form); only a terminal that understands OSC 8 sees the full
// target, so clicking anywhere on the truncated text still opens it.
//
// Truncations are processed in document order with a cursor that only ever
// moves forward: each search starts just past the previous replacement, not
// from offset 0. Two truncations can legitimately share identical display
// text (same prefix, budget cuts them at the same rune) — searching from 0
// every time would keep re-finding the first occurrence, nesting every
// later truncation's hyperlink inside the first one's and leaving the rest
// of the document's occurrences unlinked.
//
// Any truncation whose display text can't be located (should only happen if
// the reserve estimate in truncateBareURLs under-budgeted and glamour wrapped
// it after all) is returned unresolved rather than silently dropped, so the
// caller can fall back to rendering that URL untruncated.
//
// A truncation whose full URL fails osc8Safe is skipped outright — not
// reported as unresolved, since nothing needs a fallback retry: the visible,
// width-correct truncated text is already in place and stays exactly as
// rendered, just without a hyperlink wrapped around it.
func linkifyTruncations(rendered string, truncations []urlTruncation) (string, []urlTruncation) {
	var unresolved []urlTruncation
	cursor := 0
	for _, t := range truncations {
		if !osc8Safe(t.full) {
			continue
		}
		start, end, ok := findPlainRange(rendered[cursor:], t.display)
		if !ok {
			unresolved = append(unresolved, t)
			continue
		}
		start += cursor
		end += cursor
		r, g, b := hexToRGB(colorLink)
		styled := osc8Open(t.full) +
			"\x1b[4;38;2;" + strconv.Itoa(r) + ";" + strconv.Itoa(g) + ";" + strconv.Itoa(b) + "m" +
			t.display + "\x1b[0m" + osc8Close
		rendered = rendered[:start] + styled + rendered[end:]
		cursor = start + len(styled)
	}
	return rendered, unresolved
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
// needle is never empty: findPlainRange rejects an empty plain substring
// before this is reached.
func indexRunes(haystack, needle []rune) int {
	if len(needle) > len(haystack) {
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
