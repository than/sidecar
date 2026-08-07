package main

import (
	"os"
	"regexp"
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
)

func renderFixture(t *testing.T, width int) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/REVIEW.md")
	if err != nil {
		t.Fatal(err)
	}
	out, err := renderMarkdown(string(raw), width, true)
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
// in the pane and fake double-spacing. This holds even for bare-URL lines
// wider than the width: their OSC 8 target carries the full URL, but the
// visible display text is elided to fit (see TestBareURLIntact, issue #15).
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

// The hyperlinked-line path has width-sensitive elision math the rest of
// the renderer doesn't (restoreBareURLs' budget calculation), so it gets
// its own sweep across every width from the render-width clamp floor up —
// scoped to lines that actually contain a hyperlink, so it isn't tripped
// up by glamour's own pre-existing, unrelated wrap imprecision on ordinary
// text at odd widths (a real but out-of-scope issue: e.g. at width 15 a
// plain non-URL blockquote line measures one cell over, independent of
// anything in this file).
func TestHyperlinkedLineNeverWiderThanWidth(t *testing.T) {
	for w := 10; w <= 100; w++ {
		out := renderFixture(t, w)
		for i, line := range strings.Split(out, "\n") {
			if !oscLinkRE.MatchString(line) {
				continue
			}
			if got := visibleWidth(line); got > w {
				t.Errorf("width %d, line %d: visible width %d: %q",
					w, i, got, stripANSI(line))
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

// oscLinkRE finds an OSC 8 hyperlink's target and display text. The display
// text carries its own SGR styling, so it isn't ANSI-escape-free — match
// non-greedily up to the closing OSC 8 rather than excluding ESC outright.
var oscLinkRE = regexp.MustCompile(`(?s)\x1b]8;;([^\x07\n]*)\x07(.*?)\x1b]8;;\x07`)

// Bare URLs must be reachable via a single, complete OSC 8 hyperlink on one
// line — even at pane widths narrower than the URL itself, which is the
// common case and is what used to hard-split mid-URL (issue #15), and later
// what a naive fix let the viewport silently truncate instead.
func TestBareURLIntact(t *testing.T) {
	out := renderFixture(t, 40)
	for _, url := range []string{
		"https://github.com/example/app/pull/412",
		"https://qa.example.dev/checkout-race",
	} {
		found := false
		for _, line := range strings.Split(out, "\n") {
			for _, m := range oscLinkRE.FindAllStringSubmatch(line, -1) {
				if m[1] != url {
					continue
				}
				found = true
				if n := strings.Count(line, "\x1b]8;;"+url+"\x07"); n > 1 {
					t.Errorf("target %s duplicated on line: %q", url, line)
				}
			}
		}
		if !found {
			t.Errorf("URL %s has no complete OSC 8 hyperlink on one line:\n%s", url, stripANSI(out))
		}
	}
}

// linkify=false (the non-TTY path runStatic uses when stdout isn't a
// terminal) must skip the stash/hyperlink machinery entirely — a plain,
// complete URL for grep/CI/an editor, not an OSC 8 escape it can't render
// and won't see through. At runStatic's real non-TTY width (80), the
// fixture's URLs fit without wrapping at all — the common case linkify=false
// is meant to help. (At a narrower width a long URL still hits the
// original pre-#15 wrap-split via glamour's own autolink path; skipping the
// stash doesn't and isn't meant to fix that — it only avoids hiding an
// already-short URL behind an unreadable escape sequence.)
func TestLinkifyFalseSkipsHyperlinking(t *testing.T) {
	raw, err := os.ReadFile("testdata/REVIEW.md")
	if err != nil {
		t.Fatal(err)
	}
	out, err := renderMarkdown(string(raw), 78, false)
	if err != nil {
		t.Fatal(err)
	}
	if oscLinkRE.MatchString(out) {
		t.Errorf("linkify=false still produced an OSC 8 hyperlink:\n%s", stripANSI(out))
	}
	if strings.Contains(out, "\x1fU") {
		t.Errorf("a stash placeholder leaked with linkify=false:\n%s", stripANSI(out))
	}
	if !strings.Contains(stripANSI(out), "https://github.com/example/app/pull/412") {
		t.Errorf("plain URL missing with linkify=false:\n%s", stripANSI(out))
	}
}

// An ordered-list URL (`1. https://…`) is a real board shape, not just a
// bulleted one — it must get the same fix, not fall through to the
// original wrap-split bug.
func TestBareURLOrderedListMarker(t *testing.T) {
	raw := "1. https://example.test/ordered-list-item-long-enough-to-need-eliding\n"
	out, err := renderMarkdown(raw, 40, true)
	if err != nil {
		t.Fatal(err)
	}
	m := oscLinkRE.FindStringSubmatch(out)
	if m == nil || m[1] != "https://example.test/ordered-list-item-long-enough-to-need-eliding" {
		t.Errorf("ordered-list URL wasn't hyperlinked: %v\n%s", m, stripANSI(out))
	}
}

// Two bare URLs that elide to identical visible text must still each get
// their own correct hyperlink target — no cross-linking (PR #18's confirmed
// collision bug: a global text search re-found the first occurrence).
func TestBareURLCollisionSafe(t *testing.T) {
	raw := "- https://example.test/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1\n" +
		"- https://example.test/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa2\n"
	out, err := renderMarkdown(raw, 40, true)
	if err != nil {
		t.Fatal(err)
	}
	links := oscLinkRE.FindAllStringSubmatch(out, -1)
	if len(links) != 2 {
		t.Fatalf("want 2 hyperlinks, got %d:\n%s", len(links), stripANSI(out))
	}
	if links[0][1] != "https://example.test/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1" {
		t.Errorf("first link target wrong: %q", links[0][1])
	}
	if links[1][1] != "https://example.test/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa2" {
		t.Errorf("second link target wrong: %q", links[1][1])
	}
}

// A bare URL inside a fenced code block is content the user typed verbatim
// — stashBareURLs must leave it alone entirely (no OSC 8 hyperlink, no
// leaked placeholder). Whatever glamour itself does with fenced content
// (it word-wraps long hyphenated lines same as any other text — pre-existing,
// unrelated to bare-URL handling) is out of scope here.
func TestBareURLInFenceUntouched(t *testing.T) {
	raw := "```\nhttps://example.test/verbatim-in-a-fence-that-is-long-enough-to-elide\n```\n"
	out, err := renderMarkdown(raw, 40, true)
	if err != nil {
		t.Fatal(err)
	}
	if oscLinkRE.MatchString(out) {
		t.Errorf("URL inside a fence got hyperlinked:\n%s", stripANSI(out))
	}
	if strings.Contains(out, "\x1fU") {
		t.Errorf("a stash placeholder leaked into fenced output:\n%s", stripANSI(out))
	}
}

// A control byte or non-ASCII byte in a URL must not break out of the OSC 8
// escape (injection) or get silently dropped (lossy percent-encoding) \u2014 in
// EITHER half of the hyperlink. The target (m[1]) percent-encodes so the
// link stays meaningful; the display text (m[2]) just needs the control
// bytes gone outright, since a raw ESC/BEL there reaches the terminal as a
// live escape sequence (glamour never sees the stashed URL to neutralize
// it, and neither xansi.Truncate nor termenv sanitize).
func TestBareURLUnsafeBytesEncoded(t *testing.T) {
	raw := "- https://example.test/caf\u00e9-and-a-bell-\x07-in-the-middle\n"
	out, err := renderMarkdown(raw, 40, true)
	if err != nil {
		t.Fatal(err)
	}
	m := oscLinkRE.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no hyperlink found:\n%s", stripANSI(out))
	}
	if strings.ContainsAny(m[1], "\x07\x1b") {
		t.Fatalf("target still contains a raw control byte: %q", m[1])
	}
	if !strings.Contains(m[1], "%C3%A9") || !strings.Contains(m[1], "%07") {
		t.Errorf("target wasn't percent-encoded correctly: %q", m[1])
	}
	// The display text legitimately carries our own SGR styling (teal,
	// underline), which is itself ESC bytes \u2014 strip that (SGR-only; OSC 8
	// isn't SGR and survives) before checking for anything else.
	if plain := stripANSI(m[2]); strings.ContainsAny(plain, "\x07\x1b\x00") {
		t.Fatalf("display text still contains a raw control byte \u2014 escape injection: %q", plain)
	}
}

// An ESC byte in a URL must not let a pasted-in escape sequence \u2014 most
// pointedly a second, attacker-controlled OSC 8 open \u2014 reach the terminal
// inside the display text. Board files are agent-written from pasted tool
// output, so this isn't hypothetical.
func TestBareURLDisplayTextNoEscapeInjection(t *testing.T) {
	raw := "- https://example.test/x\x1b]8;;https://evil.test\x07pwned\n"
	out, err := renderMarkdown(raw, 60, true)
	if err != nil {
		t.Fatal(err)
	}
	m := oscLinkRE.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no hyperlink found:\n%s", stripANSI(out))
	}
	plain := stripANSI(m[2]) // strip our own legitimate SGR styling first
	if strings.Contains(plain, "\x1b]8;;") {
		t.Fatalf("display text carries an injected OSC 8 open: %q", plain)
	}
	if strings.Contains(plain, "\x1b") {
		t.Fatalf("display text contains a raw ESC byte: %q", plain)
	}
}

// A nested list item's budget must account for its actual indent, not a
// guessed constant (PR #18's confirmed bug: a hard-coded top-level-bullet
// reserve overshot on nested items and re-broke the line it was meant to
// fix).
func TestBareURLNestedIndentBudget(t *testing.T) {
	raw := "- Parent\n  - https://example.test/nested-item-url-thats-long-enough-to-need-eliding\n"
	out, err := renderMarkdown(raw, 30, true)
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(out, "\n") {
		if w := visibleWidth(line); w > 30 {
			t.Errorf("line %d: visible width %d > 30: %q", i, w, stripANSI(line))
		}
	}
	m := oscLinkRE.FindStringSubmatch(out)
	if m == nil || m[1] != "https://example.test/nested-item-url-thats-long-enough-to-need-eliding" {
		t.Errorf("nested URL lost its hyperlink target: %v", m)
	}
}

// A URL as its own continuation line under a 2-levels-deep nested bullet
// reaches the same 4-space indent as an indented code block, but it's a
// CommonMark lazy list continuation, not code — preceded by a non-blank
// line (the child item itself), not a blank one. It must still be
// hyperlinked, not skipped by the indented-code-block guard.
func TestBareURLNestedListContinuationNotMistakenForCode(t *testing.T) {
	raw := "- Parent\n  - Child item\n    https://example.test/nested-continuation-line-long-enough-to-elide\n"
	out, err := renderMarkdown(raw, 30, true)
	if err != nil {
		t.Fatal(err)
	}
	m := oscLinkRE.FindStringSubmatch(out)
	if m == nil || m[1] != "https://example.test/nested-continuation-line-long-enough-to-elide" {
		t.Errorf("nested list continuation URL wasn't hyperlinked (mistaken for indented code?): %v\n%s", m, stripANSI(out))
	}
}

// changedLines must still detect a change when only a URL's target differs
// but its elided display text happens to be identical — the diff key has to
// retain the OSC 8 target, not just the visible text.
func TestChangedLinesSeesURLTargetChange(t *testing.T) {
	before, err := renderMarkdown("- https://example.test/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1\n", 40, true)
	if err != nil {
		t.Fatal(err)
	}
	after, err := renderMarkdown("- https://example.test/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa2\n", 40, true)
	if err != nil {
		t.Fatal(err)
	}
	changed := changedLines(strings.Split(before, "\n"), strings.Split(after, "\n"))
	if len(changed) == 0 {
		t.Errorf("target-only URL change went undetected")
	}
}

// The gate PR #18 could never verify: bubbletea's renderer and lipgloss's
// MaxWidth both truncate the final frame to the pane width using
// charmbracelet/x/ansi, which understands OSC 8 and only cuts visible
// cells. If that ever regresses to an OSC-blind truncator, a hyperlinked
// line gets cut inside the escape and the display text (which comes after
// it) is dropped outright — this pins the assumption directly.
func TestHyperlinkSurvivesDownstreamTruncation(t *testing.T) {
	target := "https://example.test/downstream-truncation-gate"
	line := "prefix " + hyperlink(target, "short")
	got := xansi.Truncate(line, 20, "")
	m := oscLinkRE.FindStringSubmatch(got)
	if m == nil || m[1] != target {
		t.Fatalf("x/ansi.Truncate dropped or corrupted the hyperlink: %q", got)
	}
	if !strings.Contains(stripANSI(got), "short") {
		t.Errorf("x/ansi.Truncate dropped the display text: %q", got)
	}
}

// A bare-URL line that's a soft-wrapped continuation inside a multi-line
// paragraph can have real content after the URL on the same rendered line
// (e.g. "See docs at\nhttps://…\nfor more." reflows to one paragraph).
// That trailing content must survive, and the elision budget must leave it
// room — restoreBareURLs used to discard everything after the placeholder.
func TestBareURLKeepsTrailingContent(t *testing.T) {
	raw := "See the docs at\nhttps://example.test/reasonably-long-path-name\nfor more information, seriously.\n"
	out, err := renderMarkdown(raw, 40, true)
	if err != nil {
		t.Fatal(err)
	}
	plain := stripANSI(out)
	if !strings.Contains(plain, "for more") {
		t.Errorf("trailing paragraph content after the URL was dropped:\n%s", plain)
	}
	for i, line := range strings.Split(out, "\n") {
		if w := visibleWidth(line); w > 40 {
			t.Errorf("line %d: visible width %d > 40: %q", i, w, stripANSI(line))
		}
	}
}

// budget < 1 (indent alone fills the pane) must drop the URL rather than
// render a line wider than the pane — the one invariant every other line
// in this renderer holds. Exercised directly against restoreBareURLs: a
// markdown fixture that reliably produces a zero-budget nested indent
// through the real glamour pipeline is brittle to construct, and this is
// the exact boundary the fix claims to hold.
func TestBareURLNoRoomDropsRatherThanOverflows(t *testing.T) {
	prefix := strings.Repeat("x", 10) // consumes the entire width on its own
	rendered := prefix + urlPlaceholder(0)
	out := restoreBareURLs(rendered, []string{"https://example.test/no-room-left"}, 10)
	if w := visibleWidth(out); w > 10 {
		t.Errorf("visible width %d > 10: %q", w, out)
	}
	if strings.Contains(out, "\x1f") {
		t.Errorf("placeholder leaked into output: %q", out)
	}
	if strings.Contains(out, "http") {
		t.Errorf("URL text should have been dropped, not shown partially: %q", out)
	}
}

// A CRLF board must still get the fix — bareURLLine's trailing-whitespace
// class has to include \r or a CRLF board silently keeps the pre-fix
// wrap-split bug.
func TestBareURLCRLFStillStashed(t *testing.T) {
	raw := "- https://example.test/crlf-board-long-enough-to-need-eliding\r\n"
	out, err := renderMarkdown(raw, 40, true)
	if err != nil {
		t.Fatal(err)
	}
	m := oscLinkRE.FindStringSubmatch(out)
	if m == nil || m[1] != "https://example.test/crlf-board-long-enough-to-need-eliding" {
		t.Errorf("CRLF board line wasn't hyperlinked: %v\n%s", m, stripANSI(out))
	}
}

// A URL inside a 4-space indented code block is CommonMark verbatim
// content, same as a fenced block — it must not be truncated or
// hyperlinked.
func TestBareURLInIndentedCodeBlockUntouched(t *testing.T) {
	raw := "Some paragraph.\n\n    https://example.test/indented-code-block-verbatim-text\n"
	out, err := renderMarkdown(raw, 40, true)
	if err != nil {
		t.Fatal(err)
	}
	if oscLinkRE.MatchString(out) {
		t.Errorf("URL inside an indented code block got hyperlinked:\n%s", stripANSI(out))
	}
	if strings.Contains(out, "\x1fU") {
		t.Errorf("a stash placeholder leaked into indented-code-block output:\n%s", stripANSI(out))
	}
}

// Emoji section markers are double-width; wrapping must account for that.
func TestEmojiHeadingWidth(t *testing.T) {
	out, err := renderMarkdown("## 🔴 Needs action right now with a long heading tail end", 40, true)
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
