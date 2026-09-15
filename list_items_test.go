// list_items_test.go — board items were recognised only as "- " bullets, so
// an ordered or starred list rendered with a "(0)" count above visible
// content, got no ▸ change pointer, and never showed up in `sidecar diff`.
package main

import (
	"strings"
	"testing"
)

func TestParseBoardCountsEveryListMarker(t *testing.T) {
	cases := map[string]string{
		"dash":     "## 🧠 Needs you\n\n- one\n- two\n",
		"ordered":  "## 🧠 Needs you\n\n1. one\n2. two\n",
		"paren":    "## 🧠 Needs you\n\n1) one\n2) two\n",
		"star":     "## 🧠 Needs you\n\n* one\n* two\n",
		"plus":     "## 🧠 Needs you\n\n+ one\n+ two\n",
		"checkbox": "## 🧠 Needs you\n\n- [ ] one\n- [x] two\n",
		"mixed":    "## 🧠 Needs you\n\n- one\n2. two\n",
	}
	for name, raw := range cases {
		b, ok := parseBoard(raw)
		if !ok {
			t.Fatalf("%s: parseBoard rejected the board", name)
		}
		if len(b.Sections) != 1 {
			t.Fatalf("%s: want 1 section, got %d", name, len(b.Sections))
		}
		if n := len(b.Sections[0].Items); n != 2 {
			t.Errorf("%s: want 2 items, got %d", name, n)
		}
	}
}

// A continuation line under an ordered item still belongs to that item.
func TestParseBoardOrderedItemContinuation(t *testing.T) {
	raw := "## 🧠 Needs you\n\n1. one\n   https://example.test/a\n2. two\n"
	b, _ := parseBoard(raw)
	items := b.Sections[0].Items
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	if !strings.Contains(items[0].Raw, "example.test") {
		t.Errorf("continuation line lost from item 0: %q", items[0].Raw)
	}
}

// The item key drops the marker, so renumbering a list doesn't read as every
// item having changed.
func TestNormalizeItemDropsEveryMarker(t *testing.T) {
	want := "ship it"
	for _, line := range []string{"- ship it", "* ship it", "+ ship it", "1. ship it", "12) ship it"} {
		if got := normalizeItem(line); got != want {
			t.Errorf("normalizeItem(%q) = %q, want %q", line, got, want)
		}
	}
	// Renumbering must not look like a change.
	if normalizeItem("1. ship it") != normalizeItem("3. ship it") {
		t.Error("renumbering an ordered list reads as a content change")
	}
}

// The ▸ pointer keys off glamour's rendered prefix, which is "• " for
// bullets but "1. " for ordered items and "✓ "/"□ " for tasks.
func TestIsBulletLineCoversRenderedMarkers(t *testing.T) {
	for _, ln := range []string{"• item", "  • item", "1. item", "12. item", "1) item", "✓ done", "□ todo"} {
		if !isBulletLine(ln) {
			t.Errorf("isBulletLine(%q) = false, want true", ln)
		}
	}
	for _, ln := range []string{"plain text", "a • mid-sentence bullet", "▍ Heading", ""} {
		if isBulletLine(ln) {
			t.Errorf("isBulletLine(%q) = true, want false", ln)
		}
	}
}
