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
