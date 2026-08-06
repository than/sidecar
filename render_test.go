package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
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
		urlLines := 0
		for _, line := range strings.Split(plain, "\n") {
			if strings.Contains(line, "http") {
				urlLines++
			}
		}
		if urlLines != 1 {
			t.Errorf("width %d: URL text spread across %d lines: %q", width, urlLines, plain)
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
	urlLines := 0
	for _, line := range strings.Split(plain, "\n") {
		if strings.Contains(line, "http") {
			urlLines++
		}
	}
	if urlLines != 1 {
		t.Errorf("URL text spread across %d lines: %q", urlLines, plain)
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
