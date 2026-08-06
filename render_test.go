package main

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func renderFixture(t *testing.T, width int) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/REVIEW.md")
	if err != nil {
		t.Fatal(err)
	}
	out, err := renderMarkdown(string(raw), width)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// COMPACT: at most one blank line between blocks. Stock glamour "dark"
// pads every block with 2-3 blank lines — this guards the regression.
func TestCompactSpacing(t *testing.T) {
	out := renderFixture(t, 78)
	blanks := 0
	for i, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(stripANSI(line)) == "" {
			blanks++
			if blanks > 1 {
				t.Fatalf("lines %d-%d: %d consecutive blank lines", i-blanks+1, i, blanks)
			}
		} else {
			blanks = 0
		}
	}
}

// NEVER render wider than the requested width — padded/overwide lines wrap
// in the pane and fake double-spacing.
func TestNeverWiderThanWidth(t *testing.T) {
	for _, width := range []int{40, 60, 78} {
		out := renderFixture(t, width)
		for i, line := range strings.Split(out, "\n") {
			if w := visibleWidth(line); w > width {
				t.Errorf("width %d, line %d: visible width %d: %q",
					width, i, w, stripANSI(line))
			}
		}
	}
}

// No trailing-space padding on any line (glow's -w padding bug).
func TestNoTrailingSpacePadding(t *testing.T) {
	out := renderFixture(t, 78)
	for i, line := range strings.Split(out, "\n") {
		if strings.HasSuffix(line, " ") {
			t.Errorf("line %d has trailing spaces: %q", i, line)
		}
	}
}

// Bare URLs must survive intact on a single line so Ghostty's link
// detection can make them clickable.
func TestBareURLIntact(t *testing.T) {
	out := stripANSI(renderFixture(t, 78))
	for _, url := range []string{
		"https://github.com/example/app/pull/412",
		"https://qa.example.dev/checkout-race",
	} {
		found := false
		for _, line := range strings.Split(out, "\n") {
			if n := strings.Count(line, url); n > 0 {
				found = true
				if strings.Count(line, "http") > 1 {
					t.Errorf("URL duplicated on line: %q", line)
				}
			}
		}
		if !found {
			t.Errorf("URL %s not intact on a single line:\n%s", url, out)
		}
	}
}

// A bare URL wider than the render width must never be split across lines —
// glamour's word-wrap force-breaks long "words" mid-character, which turns
// one clickable URL into two dead fragments (issue #15). The full URL must
// still be reachable, as an OSC 8 hyperlink target, even though the visible
// text is truncated to fit.
func TestLongBareURLNeverWraps(t *testing.T) {
	const url = "https://github.com/example/really-long-org-name/really-long-repo-name/pull/123456"
	raw := "- " + url + "\n"

	for _, width := range []int{24, 40, 78} {
		out, err := renderMarkdown(raw, width)
		if err != nil {
			t.Fatal(err)
		}
		plain := stripANSI(out)

		// Counting lines containing "http" can't detect a wrap: a
		// force-broken continuation starts mid-path ("com/example/...",
		// "name/pull/123456") and contains no "http" at all, so that count
		// stays 1 whether or not the line actually wrapped. Assert instead
		// that the truncated display text glamour was actually asked to
		// render — computed the same way renderMarkdown computes it —
		// appears whole, as a contiguous substring, on exactly one line: if
		// a regression makes the budget too generous and glamour wraps the
		// text anyway, no single line contains it intact and this fails.
		_, truncations := truncateBareURLs(raw, width, nil)
		if len(truncations) != 1 {
			t.Fatalf("width %d: expected 1 truncation, got %d: %+v", width, len(truncations), truncations)
		}
		display := truncations[0].display
		onLines := 0
		for _, line := range strings.Split(plain, "\n") {
			if strings.Contains(line, display) {
				onLines++
			}
		}
		if onLines != 1 {
			t.Errorf("width %d: truncated display text %q intact on %d lines, want 1:\n%s", width, display, onLines, plain)
		}
		for i, line := range strings.Split(out, "\n") {
			if w := visibleWidth(line); w > width {
				t.Errorf("width %d, line %d: visible width %d exceeds width: %q", width, i, w, stripANSI(line))
			}
		}
		if !strings.Contains(out, "\x1b]8;;"+url) {
			t.Errorf("width %d: OSC 8 hyperlink target for full URL not found in:\n%q", width, out)
		}
	}
}

// Two bare URLs that collide on their truncated display text (same prefix,
// different tail) must each still get their OWN OSC 8 target — not the
// first URL's target linked twice while the second gets none. Regression
// for a reviewer-caught bug: linkifyTruncations searched from offset 0 on
// every iteration, so the second occurrence of an identical display string
// was "found" at the first occurrence's position, nesting both hyperlinks
// on line one and leaving line two dead.
func TestCollidingTruncationsBothLinked(t *testing.T) {
	common := "https://example.test/" + strings.Repeat("a", 80)
	url1 := common + "-one"
	url2 := common + "-two"
	raw := "- " + url1 + "\n- " + url2 + "\n"

	out, err := renderMarkdown(raw, 40)
	if err != nil {
		t.Fatal(err)
	}

	// Each target must land on its own line, one target per line — not both
	// nested onto the first occurrence, leaving the second display line
	// with no hyperlink of its own.
	lines := strings.Split(out, "\n")
	line1, line2 := -1, -1
	for i, line := range lines {
		has1 := strings.Contains(line, "\x1b]8;;"+url1)
		has2 := strings.Contains(line, "\x1b]8;;"+url2)
		if has1 && has2 {
			t.Fatalf("line %d carries both targets nested together: %q", i, line)
		}
		if has1 {
			line1 = i
		}
		if has2 {
			line2 = i
		}
	}
	if line1 < 0 {
		t.Errorf("first URL's OSC 8 target missing:\n%q", out)
	}
	if line2 < 0 {
		t.Errorf("second URL's OSC 8 target missing:\n%q", out)
	}
	if line1 >= 0 && line2 >= 0 {
		if line1 == line2 {
			t.Errorf("both targets landed on the same line %d", line1)
		}
		if line1 > line2 {
			t.Errorf("targets out of document order: first on line %d, second on line %d", line1, line2)
		}
	}
}

// Adversarial-panel regression: prose containing a truncation's display text
// must never steal the hyperlink. linkifyTruncations used to locate a
// truncation by searching the whole rendered document for its display text
// — the first place it turned up, even buried in an unrelated sentence,
// "won" the OSC 8 target, leaving the real bare-URL line's own occurrence
// unlinked and the full URL unrecoverable from the screen.
//
// A display string is deliberately sized close to the render width (that's
// what makes truncation necessary in the first place), so wrapping prose
// that embeds a copy of it verbatim tends to isolate that copy onto its own
// wrapped line — at which point it's genuinely indistinguishable, by
// content alone, from the real line, and the safe behavior is declining to
// link EITHER (see the count-parity doc on linkifyTruncations): the decoy
// must not get the hyperlink, and the real URL must still be fully
// recoverable via the retry fallback. TestLinkifyTruncationsIgnoresDecoyLine
// covers the complementary, unambiguous case: a decoy line's payload isn't
// EXACTLY the display text (extra words on the same line), which never
// registers as a candidate at all.
func TestProseDecoyContainingDisplayTextDoesNotStealHyperlink(t *testing.T) {
	const url = "https://example.com/very/long/path/segment/keeps/going/and/going/xyz123456789"
	const width = 40

	_, truncations := truncateBareURLs("- "+url+"\n", width, nil)
	if len(truncations) != 1 {
		t.Fatalf("expected 1 truncation, got %d: %+v", len(truncations), truncations)
	}
	display := truncations[0].display

	raw := "decoy " + display + " here\n\n- " + url + "\n"
	out, err := renderMarkdown(raw, width)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(out, "\x1b]8;;") {
		t.Errorf("a link was attached despite ambiguous decoy/real pairing:\n%q", out)
	}
	plain := stripANSI(out)
	joined := strings.ReplaceAll(strings.ReplaceAll(plain, "\n", ""), " ", "")
	if !strings.Contains(joined, strings.ReplaceAll(url, " ", "")) {
		t.Errorf("real URL did not survive the fallback retry, full text not reconstructible:\n%s", plain)
	}
}

// Adversarial-panel regression, and the end-to-end pin for the
// unresolved→retry fallback path: a literal line whose text is EXACTLY a
// truncation's display string — e.g. an agent pasting previously-truncated
// text copied back from the pane — must not be mistaken for that
// truncation's own line. There are then two candidate lines with that exact
// payload but only one real truncation, so the pairing is ambiguous; the
// truncation must come back unresolved and the retry must render the real
// URL fully, untruncated, plain (no ellipsis, no OSC 8) — never linked to
// the wrong place, and never left as inert truncated text with no link at
// all.
func TestPastedBackTruncatedTextDoesNotStealHyperlink(t *testing.T) {
	const url = "https://example.com/very/long/path/segment/keeps/going/and/going/xyz123456789"
	const width = 40

	_, truncations := truncateBareURLs("- "+url+"\n", width, nil)
	if len(truncations) != 1 {
		t.Fatalf("expected 1 truncation, got %d: %+v", len(truncations), truncations)
	}
	display := truncations[0].display

	// The pasted line is itself bare-URL-shaped (bareURLLineRE's \S+ matches
	// the ellipsis character too) and already fits the budget, so
	// truncateBareURLs leaves it exactly as written — a second, unrelated
	// line with the identical rendered payload.
	raw := "- " + display + "\n- " + url + "\n"
	out, err := renderMarkdown(raw, width)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(out, "\x1b]8;;") {
		t.Errorf("ambiguous pairing should resolve to NO hyperlink at all, found one:\n%q", out)
	}
	plain := stripANSI(out)
	joined := strings.ReplaceAll(strings.ReplaceAll(plain, "\n", ""), " ", "")
	if !strings.Contains(joined, strings.ReplaceAll(url, " ", "")) {
		t.Errorf("real URL did not survive the fallback retry, full text not reconstructible:\n%s", plain)
	}
}

// Bare URLs inside fenced code blocks are verbatim content, not board links
// — this fix must not truncate them or wrap them in a hyperlink, silently
// altering what the fence is supposed to reproduce exactly. (Glamour's own
// document-level word-wrap can still rewrap an overlong fenced line — that's
// pre-existing, independent behavior this fix doesn't touch or need to; the
// width here is generous enough that it doesn't kick in, isolating the
// check to what truncateBareURLs/linkifyTruncations do.)
func TestBareURLInFenceUntouched(t *testing.T) {
	url := "https://example.test/" + strings.Repeat("a", 80) + "/tail"
	raw := "```\n- " + url + "\n```\n"

	out, err := renderMarkdown(raw, 150)
	if err != nil {
		t.Fatal(err)
	}
	plain := stripANSI(out)
	if !strings.Contains(plain, url) {
		t.Errorf("fenced URL was altered, full text not found:\n%s", plain)
	}
	if strings.Contains(plain, "…") {
		t.Errorf("fenced URL was truncated:\n%s", plain)
	}
	if strings.Contains(out, "\x1b]8;;") {
		t.Errorf("fenced URL was hyperlinked:\n%q", out)
	}
}

// A nested bullet's extra indent must be accounted for in the truncation
// budget. Regression for a reviewer-caught bug: reserve was hard-coded to 2
// (a top-level bullet's "• "), so a nested "  - https://…" over-budgeted,
// glamour force-broke it anyway, findPlainRange couldn't locate the
// (wrongly sized) display text post-render, and the truncation was silently
// dropped — leaving inert, unlinked, truncated-looking text strictly worse
// than the pre-fix behavior.
func TestNestedBareURLNeverWraps(t *testing.T) {
	const url = "https://github.com/example/really-long-org-name/really-long-repo-name/pull/123456"
	raw := "- outer\n  - " + url + "\n"
	const width = 30

	out, err := renderMarkdown(raw, width)
	if err != nil {
		t.Fatal(err)
	}
	plain := stripANSI(out)

	// See TestLongBareURLNeverWraps: counting lines containing "http" can't
	// detect a wrap (a continuation starts mid-path); assert the actual
	// truncated display text is intact on exactly one line instead.
	_, truncations := truncateBareURLs(raw, width, nil)
	if len(truncations) != 1 {
		t.Fatalf("expected 1 truncation, got %d: %+v", len(truncations), truncations)
	}
	display := truncations[0].display
	onLines := 0
	for _, line := range strings.Split(plain, "\n") {
		if strings.Contains(line, display) {
			onLines++
		}
	}
	if onLines != 1 {
		t.Errorf("truncated display text %q intact on %d lines, want 1:\n%s", display, onLines, plain)
	}
	for i, line := range strings.Split(out, "\n") {
		if w := visibleWidth(line); w > width {
			t.Errorf("line %d: visible width %d exceeds width: %q", i, w, stripANSI(line))
		}
	}
	if !strings.Contains(out, "\x1b]8;;"+url) {
		t.Errorf("OSC 8 hyperlink target for full URL not found in:\n%q", out)
	}
}

// End-to-end regression for AB1: two renders of a board where a bare URL's
// last path segment changes (pull/17 → pull/18), but the change lands
// entirely past the truncation budget so the two renders' VISIBLE text is
// byte-for-byte identical — only the OSC 8 target differs. changedLines must
// still flag the line so the ▸ marker and flash fire; a human reading past
// the truncation would otherwise never learn the link moved.
func TestChangedLinesDetectsCollidingURLTargetChange(t *testing.T) {
	common := "https://example.test/" + strings.Repeat("a", 80) + "/pull/"
	const width = 40

	before, err := renderMarkdown("- "+common+"17\n", width)
	if err != nil {
		t.Fatal(err)
	}
	after, err := renderMarkdown("- "+common+"18\n", width)
	if err != nil {
		t.Fatal(err)
	}

	oldLines := strings.Split(before, "\n")
	newLines := strings.Split(after, "\n")
	if stripANSI(before) != stripANSI(after) {
		t.Fatalf("test setup invalid: visible text differs, not a true collision:\nbefore: %q\nafter:  %q",
			stripANSI(before), stripANSI(after))
	}

	changed := changedLines(oldLines, newLines)
	if len(changed) == 0 {
		t.Errorf("target-only change went undetected: %v", changed)
	}
}

// renderMarkdownPlain (the non-terminal path used by --static when stdout
// isn't a TTY, e.g. piped into grep) must never truncate a bare URL or wrap
// it in an OSC 8 escape: there's no terminal on the other end to resolve the
// escape, and burying the only intact copy of the URL inside one makes it
// unrecoverable by whatever's reading the pipe.
func TestRenderMarkdownPlainNoHyperlink(t *testing.T) {
	const url = "https://github.com/example/really-long-org-name/really-long-repo-name/pull/123456"
	raw := "- " + url + "\n"

	out, err := renderMarkdownPlain(raw, 24)
	if err != nil {
		t.Fatal(err)
	}
	plain := stripANSI(out)
	// This path doesn't fix issue #15's wrapping (that requires truncation,
	// which piped output can't have) — glamour may still force-break the
	// line across several rows. It must not, however, shorten the URL text
	// itself: joining the wrapped fragments back together must reproduce it
	// exactly, with no ellipsis substituted anywhere in between.
	joined := strings.ReplaceAll(strings.ReplaceAll(plain, "\n", ""), " ", "")
	if !strings.Contains(joined, strings.ReplaceAll(url, " ", "")) {
		t.Errorf("URL was altered, full text not reconstructible from wrapped lines:\n%s", plain)
	}
	if strings.Contains(plain, "…") {
		t.Errorf("URL was truncated:\n%s", plain)
	}
	if strings.Contains(out, "\x1b]8;;") {
		t.Errorf("URL was hyperlinked:\n%q", out)
	}
}

// End-to-end regression for AB3: a bare URL containing a raw control byte
// (BEL) must never end up as an OSC 8 hyperlink target — embedding it would
// let it terminate our own escape early and let the rest of the URL's bytes
// be interpreted as a new, attacker-controlled terminal escape sequence.
func TestControlByteURLNeverHyperlinked(t *testing.T) {
	url := "https://github.com/example/really-long-org-name/really-long-repo-name/pull/\x07123456"
	raw := "- " + url + "\n"

	out, err := renderMarkdown(raw, 24)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\x1b]8;;") {
		t.Errorf("control-byte URL was hyperlinked:\n%q", out)
	}
}

// End-to-end regression for AD1: an IDN/accented URL — real, non-ASCII
// content, not an attack — must still get the full truncate-and-hyperlink
// treatment, with its target percent-encoded rather than rejected outright.
func TestAccentedURLTruncatedAndLinkedPercentEncoded(t *testing.T) {
	url := "https://example.test/" + strings.Repeat("é", 40) // é is 2 bytes, 1 cell each
	raw := "- " + url + "\n"
	const width = 30

	out, err := renderMarkdown(raw, width)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "…") {
		t.Errorf("accented URL wasn't truncated:\n%q", out)
	}
	if !strings.Contains(out, "\x1b]8;;https://example.test/"+strings.Repeat("%C3%A9", 40)) {
		t.Errorf("percent-encoded OSC 8 target not found:\n%q", out)
	}
	for i, line := range strings.Split(out, "\n") {
		if w := visibleWidth(line); w > width {
			t.Errorf("line %d: visible width %d exceeds width %d: %q", i, w, width, stripANSI(line))
		}
	}
}

// End-to-end regression for AB4: a URL made of East-Asian-wide runes must
// never render wider than the pane. Measuring the fit/cut by rune count
// would consistently undercount a wide-rune URL's actual terminal width,
// letting it through untruncated and straight into glamour's own
// word-wrap — the exact wrapped-URL bug this PR exists to fix, just
// triggered by rune width instead of rune count.
func TestWideRuneURLNeverWraps(t *testing.T) {
	url := "https://example.test/" + strings.Repeat("例", 40)
	raw := "- " + url + "\n"
	const width = 30

	out, err := renderMarkdown(raw, width)
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(out, "\n") {
		if w := visibleWidth(line); w > width {
			t.Errorf("line %d: visible width %d exceeds width %d: %q", i, w, width, stripANSI(line))
		}
	}
}

// stripANSI and visibleWidth must treat OSC 8 hyperlink escapes as invisible
// — otherwise the URL embedded in the escape target gets counted as visible
// text and corrupts width checks and diff comparisons.
func TestOSC8IsInvisible(t *testing.T) {
	const url = "https://github.com/example/really-long-org-name/really-long-repo-name/pull/123456"
	raw := "- " + url + "\n"
	out, err := renderMarkdown(raw, 24)
	if err != nil {
		t.Fatal(err)
	}
	plain := stripANSI(out)
	if strings.Contains(plain, "\x1b") {
		t.Errorf("stripANSI left escape bytes: %q", plain)
	}
	for i, line := range strings.Split(out, "\n") {
		if w := visibleWidth(line); w > 24 {
			t.Errorf("line %d: visibleWidth counted hyperlink target as visible: %d: %q", i, w, line)
		}
	}
}

// Documents (and guards) a load-bearing fact about the actual render path,
// not just this package's own ANSI helpers: bubbletea v1.3.10's
// standardRenderer re-truncates every line to the pane width on each frame
// using charmbracelet/x/ansi.Truncate (standard_renderer.go:241) — a real
// OSC-aware parser, unlike muesli/reflow's CSI-only one this package works
// around elsewhere. A linkified line, once it already fits the pane width
// (guaranteed by truncateBareURLs/visibleWidth above), must pass through
// that second truncation pass unchanged: if x/ansi's parser mishandled OSC 8
// the way reflow's does, bubbletea itself would corrupt the hyperlink on
// every render even though this package's own output was correct.
func TestOSC8SurvivesBubbleteaTruncate(t *testing.T) {
	const url = "https://github.com/example/really-long-org-name/really-long-repo-name/pull/123456"
	raw := "- " + url + "\n"
	const width = 40

	out, err := renderMarkdown(raw, width)
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(out, "\n") {
		truncated := ansi.Truncate(line, width, "")
		if stripANSI(truncated) != stripANSI(line) {
			t.Errorf("line %d: bubbletea's ansi.Truncate altered visible text:\nbefore: %q\nafter:  %q",
				i, stripANSI(line), stripANSI(truncated))
		}
	}
}

// Emoji section markers are double-width; wrapping must account for that.
func TestEmojiHeadingWidth(t *testing.T) {
	out, err := renderMarkdown("## 🔴 Needs action right now with a long heading tail end", 40)
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(out, "\n") {
		if w := visibleWidth(line); w > 40 {
			t.Errorf("line %d: visible width %d > 40: %q", i, w, stripANSI(line))
		}
	}
}

// Colors must be hex, not 256-palette indexes (Ghostty remaps the palette).
func TestTrueColorOutput(t *testing.T) {
	out := renderFixture(t, 78)
	// 256-palette: ESC[38;5;Nm / ESC[48;5;Nm. Truecolor: 38;2;R;G;B.
	if pal := regexp.MustCompile(`\x1b\[[34]8;5;`).FindString(out); pal != "" {
		t.Errorf("output contains 256-palette color sequences")
	}
	if !strings.Contains(out, "[38;2;") {
		t.Errorf("output contains no truecolor sequences — profile degraded?")
	}
}
