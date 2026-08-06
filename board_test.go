// board_test.go
package main

import (
	"strings"
	"testing"
)

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

// P5: a fenced code block containing lines that look like a heading or a
// bullet must not fabricate a section or item — fence-interior lines stay
// continuation content of whatever item is open when the fence starts.
func TestParseBoardFencedCodeBlockNotParsedAsMarkup(t *testing.T) {
	raw := "## 🧠 Needs action\n\n- Review PR #7\n```\n## fake\n- fake\n```\n"
	b, ok := parseBoard(raw)
	if !ok {
		t.Fatal("expected ok")
	}
	if len(b.Sections) != 1 {
		t.Fatalf("sections = %d, want 1 (fence must not fabricate a section)", len(b.Sections))
	}
	if len(b.Sections[0].Items) != 1 {
		t.Fatalf("items = %d, want 1 (fence must not fabricate an item)", len(b.Sections[0].Items))
	}
	want := "- Review PR #7\n```\n## fake\n- fake\n```"
	if b.Sections[0].Items[0].Raw != want {
		t.Errorf("raw = %q, want %q (fence content as continuation)", b.Sections[0].Items[0].Raw, want)
	}
}

// P5: a fence with no item open (e.g. between items, after a blank line)
// still must not fabricate a section or item, even though there's nowhere
// for its content to attach.
func TestParseBoardFencedCodeBlockWithNoOpenItem(t *testing.T) {
	raw := "## 🧠 Needs action\n\n- Review PR #7\n\n```\n## fake\n- fake\n```\n\n- Ship v2\n"
	b, ok := parseBoard(raw)
	if !ok {
		t.Fatal("expected ok")
	}
	if len(b.Sections) != 1 {
		t.Fatalf("sections = %d, want 1", len(b.Sections))
	}
	if len(b.Sections[0].Items) != 2 {
		t.Fatalf("items = %d, want 2 (Review PR #7, Ship v2)", len(b.Sections[0].Items))
	}
}

// V2: an unclosed "<!--" inside a fenced code block must not open comment
// mode — the fence check must win, or every line after it (including
// sections and items past the fence) gets swallowed waiting for a "-->"
// that may never come.
func TestParseBoardFenceContainingUnclosedComment(t *testing.T) {
	raw := "## 🧠 Needs action\n\n" +
		"- Review PR #7\n" +
		"```\n" +
		"<!-- not a real comment, just fence content\n" +
		"```\n\n" +
		"## 🚧 In progress\n\n" +
		"- Ship v2\n"
	b, ok := parseBoard(raw)
	if !ok {
		t.Fatal("expected ok")
	}
	if len(b.Sections) != 2 {
		t.Fatalf("sections = %d, want 2 — everything after the fence was swallowed", len(b.Sections))
	}
	if len(b.Sections[0].Items) != 1 || b.Sections[0].Items[0].Key != "Review PR #7" {
		t.Errorf("section 0 items = %+v, want [Review PR #7]", b.Sections[0].Items)
	}
	if len(b.Sections[1].Items) != 1 || b.Sections[1].Items[0].Key != "Ship v2" {
		t.Errorf("section 1 items = %+v, want [Ship v2]", b.Sections[1].Items)
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

func TestParseBoardLineRanges(t *testing.T) {
	raw := "# Sidecar\n\n## 🧠 Needs action\n\n- Review PR #7\n  https://example.com/pr/7\n- Fix typo\n\n## ✅ Done\n\n- nothing yet\n"
	b, ok := parseBoard(raw)
	if !ok {
		t.Fatal("expected ok")
	}
	lines := strings.Split(raw, "\n")

	if b.Sections[0].HeaderLine != 2 || lines[b.Sections[0].HeaderLine] != "## 🧠 Needs action" {
		t.Errorf("section 0 HeaderLine = %d (%q), want 2", b.Sections[0].HeaderLine, lines[b.Sections[0].HeaderLine])
	}
	if b.Sections[1].HeaderLine != 8 || lines[b.Sections[1].HeaderLine] != "## ✅ Done" {
		t.Errorf("section 1 HeaderLine = %d (%q), want 8", b.Sections[1].HeaderLine, lines[b.Sections[1].HeaderLine])
	}

	item0 := b.Sections[0].Items[0] // "- Review PR #7" + continuation URL line
	if item0.StartLine != 4 || item0.EndLine != 5 {
		t.Errorf("item0 range = [%d,%d], want [4,5]", item0.StartLine, item0.EndLine)
	}
	item1 := b.Sections[0].Items[1] // "- Fix typo", no continuation
	if item1.StartLine != 6 || item1.EndLine != 6 {
		t.Errorf("item1 range = [%d,%d], want [6,6]", item1.StartLine, item1.EndLine)
	}
}
