package main

import (
	"strings"
	"testing"
)

func TestTruncateBareURLsLeavesShortURLsAlone(t *testing.T) {
	raw := "- https://example.com/short\n"
	out, truncations := truncateBareURLs(raw, 78, nil)
	if out != raw {
		t.Errorf("short URL line rewritten: %q", out)
	}
	if len(truncations) != 0 {
		t.Errorf("unexpected truncations: %+v", truncations)
	}
}

func TestTruncateBareURLsIgnoresInlineURLs(t *testing.T) {
	url := "https://github.com/example/really-long-org-name/really-long-repo-name/pull/123456"
	raw := "See " + url + " for details\n"
	out, truncations := truncateBareURLs(raw, 40, nil)
	if out != raw {
		t.Errorf("inline URL prose rewritten: %q", out)
	}
	if len(truncations) != 0 {
		t.Errorf("unexpected truncations for inline URL: %+v", truncations)
	}
}

func TestTruncateBareURLsShortensToFit(t *testing.T) {
	url := "https://github.com/example/really-long-org-name/really-long-repo-name/pull/123456"
	raw := "- " + url + "\n"
	out, truncations := truncateBareURLs(raw, 24, nil)
	if len(truncations) != 1 {
		t.Fatalf("expected 1 truncation, got %d: %+v", len(truncations), truncations)
	}
	tr := truncations[0]
	if tr.full != url {
		t.Errorf("full URL mismatch: %q", tr.full)
	}
	if got, want := "- "+tr.display+"\n", out; got != want {
		t.Errorf("rewritten line mismatch: got %q want %q", got, want)
	}
	if w := visibleWidth(tr.display); w > 24-2 {
		t.Errorf("display text too wide: %d: %q", w, tr.display)
	}
}

func TestTruncateBareURLsSkipsFencedCode(t *testing.T) {
	url := "https://github.com/example/really-long-org-name/really-long-repo-name/pull/123456"
	raw := "```\n- " + url + "\n```\n"
	out, truncations := truncateBareURLs(raw, 24, nil)
	if out != raw {
		t.Errorf("fenced URL rewritten: %q", out)
	}
	if len(truncations) != 0 {
		t.Errorf("unexpected truncations inside fence: %+v", truncations)
	}
}

func TestTruncateBareURLsFenceToggleTildeAndBacktick(t *testing.T) {
	url := "https://github.com/example/really-long-org-name/really-long-repo-name/pull/123456"
	// A tilde fence, then back out to normal markdown with the same long
	// URL — only the second occurrence should be truncated.
	raw := "~~~\n- " + url + "\n~~~\n- " + url + "\n"
	_, truncations := truncateBareURLs(raw, 24, nil)
	if len(truncations) != 1 {
		t.Fatalf("expected 1 truncation (fence closed before second URL), got %d: %+v", len(truncations), truncations)
	}
}

func TestTruncateBareURLsSkipsListedURLs(t *testing.T) {
	url := "https://github.com/example/really-long-org-name/really-long-repo-name/pull/123456"
	raw := "- " + url + "\n"
	out, truncations := truncateBareURLs(raw, 24, map[string]bool{url: true})
	if out != raw {
		t.Errorf("skipped URL rewritten: %q", out)
	}
	if len(truncations) != 0 {
		t.Errorf("unexpected truncations for skipped URL: %+v", truncations)
	}
}

// Reserve must grow with the source line's leading whitespace (nesting), not
// stay pinned at a top-level bullet's fixed "• " width — otherwise a nested
// bullet's budget is too generous and glamour wraps it anyway.
func TestTruncateBareURLsReservesNestedIndent(t *testing.T) {
	url := "https://github.com/example/really-long-org-name/really-long-repo-name/pull/123456"
	const width = 30

	_, top := truncateBareURLs("- "+url+"\n", width, nil)
	_, nested := truncateBareURLs("  - "+url+"\n", width, nil)
	if len(top) != 1 || len(nested) != 1 {
		t.Fatalf("expected one truncation each: top=%+v nested=%+v", top, nested)
	}
	if len([]rune(nested[0].display)) >= len([]rune(top[0].display)) {
		t.Errorf("nested display text (%q) should be shorter than top-level (%q) — reserve didn't grow with indent",
			nested[0].display, top[0].display)
	}
}

// A run of ~~~ inside a ``` block (or vice versa) is fence CONTENT, not a
// fence delimiter — it must not flip fence state early and let the "closed"
// remainder of the block through to truncation/linkification.
func TestTruncateBareURLsFenceCharMustMatch(t *testing.T) {
	url := "https://github.com/example/really-long-org-name/really-long-repo-name/pull/123456"
	raw := "```\n~~~\n- " + url + "\n```\n"
	_, truncations := truncateBareURLs(raw, 24, nil)
	if len(truncations) != 0 {
		t.Errorf("URL inside ``` block (after an unrelated ~~~ line) was truncated: %+v", truncations)
	}
}

func TestOsc8SafeRejectsControlBytes(t *testing.T) {
	if osc8Safe("https://example.test/\x07inject") {
		t.Error("URL containing BEL should be unsafe")
	}
	if osc8Safe("https://example.test/\x1b]2;pwned\x07") {
		t.Error("URL containing ESC should be unsafe")
	}
	if !osc8Safe("https://example.test/fine") {
		t.Error("plain ASCII URL should be safe")
	}
}

// A control byte in the URL (BEL here) must never reach an OSC 8 escape —
// embedding it verbatim would let it terminate the escape early and inject
// arbitrary terminal control sequences from markdown content. The line's
// visible, width-correct truncated text is still fine to keep (see osc8Safe's
// doc comment) — it's the hyperlink specifically that must not exist.
func TestLinkifyTruncationsSkipsUnsafeURL(t *testing.T) {
	rendered := "\x1b[38;2;1;2;3mtext\x1b[0m"
	truncations := []urlTruncation{
		{display: "text", full: "https://example.test/\x07inject"},
	}
	out, unresolved := linkifyTruncations(rendered, truncations)
	if len(unresolved) != 0 {
		t.Errorf("unsafe URL should not be reported unresolved: %+v", unresolved)
	}
	if strings.Contains(out, "\x1b]8;;") {
		t.Errorf("unsafe URL was hyperlinked:\n%q", out)
	}
	if out != rendered {
		t.Errorf("output changed for a skipped hyperlink:\ngot  %q\nwant %q", out, rendered)
	}
}

// A URL containing East-Asian-wide runes must be measured, and cut, by
// terminal cell width — not rune count. A rune-count budget would think a
// wide-rune URL fits when it actually renders twice as wide per rune,
// letting it through untruncated straight into glamour's own word-wrap.
func TestTruncateBareURLsCutsWideRunesByCellWidth(t *testing.T) {
	// Each “例” is 1 rune but 2 terminal cells; 40 of them is 40 runes but
	// 80 cells — comfortably over any width used in these tests.
	url := "https://example.test/" + strings.Repeat("例", 40)
	const width = 30

	_, truncations := truncateBareURLs("- "+url+"\n", width, nil)
	if len(truncations) != 1 {
		t.Fatalf("expected 1 truncation, got %d: %+v", len(truncations), truncations)
	}
	display := truncations[0].display
	if w := visibleWidth(display); w > width-2 {
		t.Errorf("display text %d cells wide, exceeds budget (width %d): %q", w, width, display)
	}
	// A rune-count budget would have kept far more than this many runes
	// (each wrongly assumed to cost 1 cell instead of 2).
	if n := len([]rune(display)); n > width-2 {
		t.Errorf("display text kept %d runes at width %d — looks rune-counted, not cell-counted: %q", n, width, display)
	}
}

func TestLinkifyTruncationsCollidingDisplayText(t *testing.T) {
	styled := func(text string) string {
		return "\x1b[38;2;1;2;3m" + text + "\x1b[0m"
	}
	rendered := styled("same") + "\n" + styled("same")
	truncations := []urlTruncation{
		{display: "same", full: "https://example.test/one"},
		{display: "same", full: "https://example.test/two"},
	}
	out, unresolved := linkifyTruncations(rendered, truncations)
	if len(unresolved) != 0 {
		t.Fatalf("unexpected unresolved: %+v", unresolved)
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], "\x1b]8;;https://example.test/one") || strings.Contains(lines[0], "https://example.test/two") {
		t.Errorf("line 0 should link only to /one: %q", lines[0])
	}
	if !strings.Contains(lines[1], "\x1b]8;;https://example.test/two") || strings.Contains(lines[1], "https://example.test/one") {
		t.Errorf("line 1 should link only to /two: %q", lines[1])
	}
}

func TestFindPlainRangeSkipsANSI(t *testing.T) {
	styled := "\x1b[38;2;1;2;3mhello \x1b[0m\x1b[1mworld\x1b[0m"
	start, end, ok := findPlainRange(styled, "hello world")
	if !ok {
		t.Fatal("expected match")
	}
	if styled[start:end] != "hello \x1b[0m\x1b[1mworld" {
		t.Errorf("unexpected byte range: %q", styled[start:end])
	}
}

func TestFindPlainRangeNoMatch(t *testing.T) {
	if _, _, ok := findPlainRange("\x1b[0mhello\x1b[0m", "goodbye"); ok {
		t.Error("expected no match")
	}
}
