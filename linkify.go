// linkify.go
package main

import (
	"fmt"
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
	line    int    // index into rawIn's line split this truncation came from
}

// fenceRE matches a fenced-code-block delimiter line (``` or ~~~, at most 3
// leading spaces — CommonMark's own limit before a fence marker is instead
// content or an indented code block) and captures the run of fence
// characters, so both its character and its length can be checked against
// the opener: CommonMark closes a fence only on a line of the SAME
// character, at least as LONG as the opener.
var fenceRE = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")

// leadingColumns returns the tab-expanded column width of line's leading
// run of spaces and tabs — CommonMark expands a tab to the next multiple of
// 4 when computing block-structure indentation, not to a single column.
func leadingColumns(line string) int {
	col := 0
	for _, r := range line {
		switch r {
		case ' ':
			col++
		case '\t':
			col += 4 - col%4
		default:
			return col
		}
	}
	return col
}

// osc8Target returns the string safe to embed as the target of an OSC 8
// escape for url, and whether url can be linked at all.
//
// Bytes in the OSC 8 URI's required printable-ASCII range (0x20–0x7E) pass
// through unchanged. Bytes above that range are legitimate UTF-8 for a
// non-ASCII/IDN URL (an accented domain, a CJK path, ...) — real content,
// not an attack — so they're percent-encoded, the standard way to embed
// non-ASCII bytes in a URI, rather than rejected outright.
//
// C0 control bytes (0x00–0x1F) and DEL (0x7F) return ok=false instead:
// bareURLLineRE's "\S+" happily matches a raw BEL or ESC if the source
// markdown contains one, and embedding it verbatim in
// "\x1b]8;;<url>\x07" would let it terminate the escape early (or start a
// new one) and inject arbitrary terminal control sequences into the
// render. Percent-encoding wouldn't help here — it's the byte's role as a
// terminal control character that's dangerous, not its role as URI content
// — so these are excluded rather than escaped.
//
// This gates only the hyperlink attachment in linkifyTruncations, not the
// display-text truncation in truncateBareURLs: the truncated text is plain
// markdown, rendered exactly the way glamour already renders any bare URL's
// raw bytes today — no new escape sequence is built from it, so no new
// injection surface.
func osc8Target(url string) (target string, ok bool) {
	var b strings.Builder
	for i := 0; i < len(url); i++ {
		c := url[i]
		switch {
		case c < 0x20 || c == 0x7f:
			return "", false
		case c <= 0x7e:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String(), true
}

// truncateBareURLs rewrites raw markdown so that any bare-URL-only line
// wider than width is shortened to fit, with a trailing ellipsis.
//
// Left untouched, on top of lines that already fit:
//
//   - Fenced code blocks (``` or ~~~ — see fenceRE): verbatim content, and
//     the fence is only recognized as SUCH when it's the right character and
//     at least as long as its opener, matching CommonMark's own closing
//     rule — a shorter or differently-charactered run of backticks/tildes
//     inside the block is just content, not a closer.
//
//   - Indented code blocks: any line whose leading whitespace is 4+
//     tab-expanded columns is verbatim per CommonMark (this also covers a
//     single leading tab, which expands to a full 4-column tab stop on its
//     own — so a tab-indented "nested bullet" is never a truncation
//     candidate to begin with, rather than being budgeted with a fractional
//     tab-to-column guess that could under-count it).
//
// skip holds RAW LINE INDICES (not URLs — see renderMarkdown for why) to
// leave untruncated regardless of width, used to retry a specific line
// whose truncated form couldn't be relocated after rendering.
//
// Returns the rewritten markdown and the list of shortenings made, so the
// caller can re-attach the full URL as an OSC 8 target after rendering.
func truncateBareURLs(raw string, width int, skip map[int]bool) (string, []urlTruncation) {
	lines := strings.Split(raw, "\n")
	var truncations []urlTruncation
	fenceChar := byte(0) // 0 when not inside a fence; '`' or '~' while inside one
	fenceLen := 0        // length of the run that opened the current fence
	for i, line := range lines {
		indentCols := leadingColumns(line)

		if indentCols < 4 {
			if m := fenceRE.FindStringSubmatch(line); m != nil {
				marker := m[1]
				switch {
				case fenceChar == 0:
					fenceChar, fenceLen = marker[0], len(marker)
				case marker[0] == fenceChar && len(marker) >= fenceLen:
					fenceChar, fenceLen = 0, 0
				// Different character, or too short to close: this is
				// fence CONTENT (e.g. a ``` example shown inside a ````
				// block), not a delimiter — state doesn't change.
				default:
				}
				continue
			}
		}
		// A 4+-column-indented fence marker isn't a fence at all per
		// CommonMark — it falls through to the indentCols>=4 check below
		// and is treated as (or stays) verbatim content instead.

		if fenceChar != 0 || indentCols >= 4 {
			continue
		}

		if skip[i] {
			continue
		}
		m := bareURLLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		prefix, url := m[1], m[2]
		// Reserve the columns glamour will spend on indent + bullet: any
		// leading whitespace in the source (one level of nesting per 2
		// cols, matching styleConfig's List.LevelIndent) plus "• " for the
		// marker itself. A plain, unbulleted line reserves nothing. Every
		// leading-whitespace byte counted here is a space — a tab would
		// already have hit the indentCols>=4 skip above — so a plain byte
		// count is an exact column count, no tab-stop math needed.
		reserve := 0
		if prefix != "" {
			leadingWS := len(prefix) - len(strings.TrimLeft(prefix, " "))
			reserve = leadingWS + 2
		}
		budget := width - reserve
		// Nesting can push reserve arbitrarily high (unlike a hard-coded
		// small reserve), so this floor stays reachable: without it, deeply
		// nested lines could get a negative/zero budget.
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
		// The ellipsis itself costs cells too — U+2026 is East Asian
		// Ambiguous, so under an EastAsianWidth/CJK locale (RUNEWIDTH_EASTASIAN,
		// or just a wide terminal font convention) go-runewidth measures it
		// as 2 cells, not 1. Hardcoding "-1" here would leave the assembled
		// display text one cell over budget in exactly that locale — wrapping
		// anyway and falling back to the pre-#15-fix bug for every long URL.
		ellipsisWidth := runewidth.RuneWidth('…')
		display := cutToCellWidth(url, budget-ellipsisWidth) + "…"
		lines[i] = prefix + display
		truncations = append(truncations, urlTruncation{display: display, full: url, line: i})
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

// linkifyTruncations wraps each truncation's display text, on the rendered
// line it belongs to, in a colorLink-styled OSC 8 hyperlink pointing at the
// full URL. The visible text is unchanged (still the ellipsized form); only
// a terminal that understands OSC 8 sees the full target, so clicking
// anywhere on the truncated text still opens it.
//
// Truncations are anchored to a LINE, not a substring search over the whole
// rendered document. A line is a candidate for truncation t only if its
// entire visible payload — stripped of ANSI, then of the bullet/indent
// prefix glamour renders ("• ", possibly preceded by nesting spaces) — is
// EXACTLY t.display: nothing before it, nothing after. That rules out
// matching a display string that merely appears as a SUBSTRING of some
// other line (prose mentioning it, a decoy, ...), which a plain
// document-wide substring search can't tell apart from the real line.
//
// Truncations sharing an identical display string (a legitimate collision:
// same URL prefix, same budget cut) are paired with their candidate lines
// by POSITION, in document order on both sides — rendering preserves
// source order, so the i-th truncation with a given display corresponds to
// the i-th rendered line with that exact payload. If the two counts for a
// display don't match — a decoy or pasted-back line inflating the
// candidates, or a wrap swallowing one of the truncations and shrinking
// them — there's no way to pair them without guessing, so EVERY truncation
// sharing that display is reported unresolved instead: the caller's retry
// re-renders those specific lines untruncated rather than risk linking (or
// leaving unlinked) the wrong one.
func linkifyTruncations(rendered string, truncations []urlTruncation) (string, []urlTruncation) {
	if len(truncations) == 0 {
		return rendered, nil
	}

	lines := strings.Split(rendered, "\n")
	candidatesByPayload := map[string][]int{}
	for i, ln := range lines {
		if p := lineURLPayload(ln); p != "" {
			candidatesByPayload[p] = append(candidatesByPayload[p], i)
		}
	}

	type group struct {
		truncs []urlTruncation
	}
	groups := map[string]*group{}
	var order []string
	for _, t := range truncations {
		g, ok := groups[t.display]
		if !ok {
			g = &group{}
			groups[t.display] = g
			order = append(order, t.display)
		}
		g.truncs = append(g.truncs, t)
	}

	var unresolved []urlTruncation
	replacement := make(map[int]string, len(truncations))
	for _, display := range order {
		g := groups[display]
		candidates := candidatesByPayload[display]
		if len(candidates) != len(g.truncs) {
			unresolved = append(unresolved, g.truncs...)
			continue
		}
		for i, t := range g.truncs {
			lineIdx := candidates[i]
			target, safe := osc8Target(t.full)
			if !safe {
				continue // leave this line's plain truncated text exactly as rendered
			}
			start, end, ok := findPlainRange(lines[lineIdx], t.display)
			if !ok {
				// Should be unreachable: candidates were selected because
				// their whole payload equals t.display, so it must be
				// findable within that same line. Treat as unresolved
				// rather than silently leaving it unlinked.
				unresolved = append(unresolved, t)
				continue
			}
			r, g8, b := hexToRGB(colorLink)
			styled := osc8Open(target) +
				"\x1b[4;38;2;" + strconv.Itoa(r) + ";" + strconv.Itoa(g8) + ";" + strconv.Itoa(b) + "m" +
				t.display + "\x1b[0m" + osc8Close
			replacement[lineIdx] = lines[lineIdx][:start] + styled + lines[lineIdx][end:]
		}
	}
	for i, ln := range replacement {
		lines[i] = ln
	}
	return strings.Join(lines, "\n"), unresolved
}

// lineURLPayload returns line's entire visible content with ANSI stripped
// and, if present, exactly one glamour item bullet ("• ", possibly preceded
// by nesting spaces) trimmed off the front — i.e. what a bare-URL-only
// source line renders as, minus the decoration truncateBareURLs already
// accounted for in its reserve. Returns "" for a blank line (never a valid
// truncation match).
func lineURLPayload(line string) string {
	s := strings.TrimRight(stripANSI(line), " ")
	s = strings.TrimLeft(s, " ")
	s = strings.TrimPrefix(s, "• ")
	return s
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
