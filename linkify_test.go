package main

import "testing"

func TestTruncateBareURLsLeavesShortURLsAlone(t *testing.T) {
	raw := "- https://example.com/short\n"
	out, truncations := truncateBareURLs(raw, 78)
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
	out, truncations := truncateBareURLs(raw, 40)
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
	out, truncations := truncateBareURLs(raw, 24)
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
