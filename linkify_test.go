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
