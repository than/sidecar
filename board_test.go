// board_test.go
package main

import "testing"

const sampleBoard = `# Sidecar

<!--
Sidecar review queue — agent: keep this current as you work.
· Move each item to the section matching its state.
-->

## 🧠 Needs action

- Review PR #7
  https://example.com/pr/7

## 🚧 In progress

- Fix the parser
- Ship v2

## ✅ Done

- nothing yet
`

func TestParseBoardSectionsAndItems(t *testing.T) {
	b, ok := parseBoard(sampleBoard)
	if !ok {
		t.Fatal("expected ok for a board with headings")
	}
	if len(b.Sections) != 3 {
		t.Fatalf("sections = %d, want 3", len(b.Sections))
	}
	if b.Sections[0].Label != "🧠 Needs action" {
		t.Errorf("label = %q", b.Sections[0].Label)
	}
	if len(b.Sections[1].Items) != 2 {
		t.Fatalf("in-progress items = %d, want 2", len(b.Sections[1].Items))
	}
	if b.Sections[1].Items[0].Key != "Fix the parser" {
		t.Errorf("key = %q", b.Sections[1].Items[0].Key)
	}
}

func TestParseBoardContinuationLines(t *testing.T) {
	b, _ := parseBoard(sampleBoard)
	raw := b.Sections[0].Items[0].Raw
	if want := "- Review PR #7\n  https://example.com/pr/7"; raw != want {
		t.Errorf("raw = %q, want %q", raw, want)
	}
	// Key is the first line only, bullet stripped, whitespace collapsed.
	if b.Sections[0].Items[0].Key != "Review PR #7" {
		t.Errorf("key = %q", b.Sections[0].Items[0].Key)
	}
}

func TestParseBoardCommentIgnored(t *testing.T) {
	b, _ := parseBoard(sampleBoard)
	for _, s := range b.Sections {
		for _, it := range s.Items {
			if it.Key == "· Move each item to the section matching its state." {
				t.Error("comment content parsed as an item")
			}
		}
	}
}

func TestParseBoardNoHeadings(t *testing.T) {
	if _, ok := parseBoard("just some prose\n- a stray bullet\n"); ok {
		t.Error("expected ok=false for a file with no ## headings")
	}
}

// R7: a complete single-line HTML comment must be skipped like a multi-line
// one — editing it shouldn't attach its text to the preceding item's Raw and
// report a spurious "edited" on that item.
func TestParseBoardSingleLineCommentSkipped(t *testing.T) {
	raw := "## 🧠 Needs action\n\n- Review PR #7\n<!-- a note -->\n- Ship v2\n"
	b, ok := parseBoard(raw)
	if !ok {
		t.Fatal("expected ok")
	}
	if len(b.Sections[0].Items) != 2 {
		t.Fatalf("items = %d, want 2", len(b.Sections[0].Items))
	}
	if b.Sections[0].Items[0].Raw != "- Review PR #7" {
		t.Errorf("comment line leaked into preceding item's Raw: %q", b.Sections[0].Items[0].Raw)
	}
}

func TestNormalizeItem(t *testing.T) {
	if got := normalizeItem("-   Fix   the  parser  "); got != "Fix the parser" {
		t.Errorf("got %q", got)
	}
}

// F5: a tab-indented sub-bullet is a continuation line, not a new top-level
// item — a leading tab must be treated like a leading space.
func TestParseBoardTabIndentedSubBullet(t *testing.T) {
	raw := "## 🧠 Needs action\n\n- Top item\n\t- sub\n"
	b, ok := parseBoard(raw)
	if !ok {
		t.Fatal("expected ok")
	}
	if len(b.Sections) != 1 {
		t.Fatalf("sections = %d, want 1", len(b.Sections))
	}
	if len(b.Sections[0].Items) != 1 {
		t.Fatalf("items = %d, want 1 (tab-indented line parsed as top-level)", len(b.Sections[0].Items))
	}
	want := "- Top item\n\t- sub"
	if b.Sections[0].Items[0].Raw != want {
		t.Errorf("raw = %q, want %q", b.Sections[0].Items[0].Raw, want)
	}
}
