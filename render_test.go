package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

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
		"https://github.com/broomfitters/house/pull/412",
		"https://qa.broomfitters.dev/checkout-race",
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
