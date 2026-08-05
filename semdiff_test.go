package main

import (
	"strings"
	"testing"
)

func board(t *testing.T, raw string) Board {
	t.Helper()
	b, ok := parseBoard(raw)
	if !ok {
		t.Fatal("test board failed to parse")
	}
	return b
}

func TestSemanticDiffMoved(t *testing.T) {
	old := board(t, "## 🚧 In progress\n\n- Ship v2\n\n## ✅ Done\n\n- nothing yet\n")
	new := board(t, "## 🚧 In progress\n\n- nothing left\n\n## ✅ Done\n\n- Ship v2\n")
	out := semanticDiff(old, new)
	want := `moved 🚧→✅: "Ship v2"`
	if len(out) == 0 || out[0] != want {
		t.Fatalf("out = %q, want first line %q", out, want)
	}
}

func TestSemanticDiffAddedRemoved(t *testing.T) {
	old := board(t, "## 🧠 Needs action\n\n- Old idea\n")
	new := board(t, "## 🧠 Needs action\n\n- Fresh idea\n")
	out := strings.Join(semanticDiff(old, new), "\n")
	if !strings.Contains(out, `added 🧠: "Fresh idea"`) {
		t.Errorf("missing added line:\n%s", out)
	}
	if !strings.Contains(out, `removed 🧠: "Old idea"`) {
		t.Errorf("missing removed line:\n%s", out)
	}
}

func TestSemanticDiffEditedByPrefix(t *testing.T) {
	old := board(t, "## 🚘 Parked\n\n- Picker: allow clearing an emoji\n")
	new := board(t, "## 🚘 Parked\n\n- Picker: allow clearing an emoji or hint\n")
	out := semanticDiff(old, new)
	want := `edited 🚘: "Picker: allow clearing an emoji or hint"`
	if len(out) != 1 || out[0] != want {
		t.Fatalf("out = %q, want [%q]", out, want)
	}
}

func TestSemanticDiffContinuationEdit(t *testing.T) {
	old := board(t, "## 🧠 Needs action\n\n- Review PR #7\n  first note\n")
	new := board(t, "## 🧠 Needs action\n\n- Review PR #7\n  a different note\n")
	out := semanticDiff(old, new)
	want := `edited 🧠: "Review PR #7"`
	if len(out) != 1 || out[0] != want {
		t.Fatalf("out = %q, want [%q]", out, want)
	}
}

func TestSemanticDiffUnchanged(t *testing.T) {
	b := board(t, "## 🧠 Needs action\n\n- Same item\n")
	if out := semanticDiff(b, b); len(out) != 0 {
		t.Errorf("expected no output, got %q", out)
	}
}

// R1: pass 1 must not match duplicate keys ("- nothing yet") across sections.
// Replacing only 🧠's placeholder with a real item must not fabricate a moved
// line by matching a same-key placeholder still sitting in 🚧 or ✅.
func TestSemanticDiffDuplicateKeyStaysInSection(t *testing.T) {
	old := board(t, "## 🧠 Needs action\n\n- nothing yet\n\n## 🚧 In progress\n\n- nothing yet\n\n## ✅ Done\n\n- nothing yet\n")
	new := board(t, "## 🧠 Needs action\n\n- Fix the parser\n\n## 🚧 In progress\n\n- nothing yet\n\n## ✅ Done\n\n- nothing yet\n")
	out := semanticDiff(old, new)
	want := []string{`added 🧠: "Fix the parser"`, `removed 🧠: "nothing yet"`}
	if len(out) != len(want) {
		t.Fatalf("out = %q, want %q", out, want)
	}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, out[i], want[i])
		}
	}
}

func TestSemanticDiffTextOnlySectionTag(t *testing.T) {
	old := board(t, "## Todo\n\n- nothing yet\n")
	new := board(t, "## Todo\n\n- A task\n")
	out := strings.Join(semanticDiff(old, new), "\n")
	if !strings.Contains(out, `added Todo: "A task"`) {
		t.Errorf("text-only section tag wrong:\n%s", out)
	}
}

func TestSectionTag(t *testing.T) {
	if got := sectionTag("🧠 Needs action"); got != "🧠" {
		t.Errorf("emoji tag = %q", got)
	}
	if got := sectionTag("Todo"); got != "Todo" {
		t.Errorf("text tag = %q", got)
	}
}

func TestSemanticDiffLongTitleTruncated(t *testing.T) {
	long := strings.Repeat("x", 80)
	old := board(t, "## 🧠 Needs action\n\n- nothing yet\n")
	new := board(t, "## 🧠 Needs action\n\n- "+long+"\n")
	out := strings.Join(semanticDiff(old, new), "\n")
	if !strings.Contains(out, strings.Repeat("x", 59)+"…") || strings.Contains(out, strings.Repeat("x", 60)) {
		t.Errorf("title not truncated to 59+…:\n%s", out)
	}
}

func TestUnifiedU0(t *testing.T) {
	out := unifiedU0("a\nb\nc\n", "a\nX\nc\n")
	want := []string{"@@ -2 +2 @@", "-b", "+X"}
	if len(out) != len(want) {
		t.Fatalf("out = %q, want %q", out, want)
	}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, out[i], want[i])
		}
	}
}

func TestUnifiedU0Insert(t *testing.T) {
	out := strings.Join(unifiedU0("a\nc\n", "a\nb\nc\n"), "\n")
	if !strings.Contains(out, "+b") {
		t.Errorf("missing +b:\n%s", out)
	}
}

func TestDiffLinesSemanticWhenParsable(t *testing.T) {
	oldRaw := "## 🧠 Needs action\n\n- Old idea\n"
	newRaw := "## 🧠 Needs action\n\n- New idea\n"
	out := strings.Join(diffLines(oldRaw, newRaw), "\n")
	if !strings.Contains(out, "added 🧠:") || strings.Contains(out, "@@") {
		t.Errorf("expected semantic output:\n%s", out)
	}
}

func TestDiffLinesFallbackWhenUnparsable(t *testing.T) {
	out := strings.Join(diffLines("plain old\n", "plain new\n"), "\n")
	if !strings.Contains(out, "-plain old") || !strings.Contains(out, "+plain new") {
		t.Errorf("expected textual fallback:\n%s", out)
	}
}

func TestUnifiedU0InsertAtStartOfNonEmptyFile(t *testing.T) {
	out := unifiedU0("c\n", "x\nc\n")
	if len(out) == 0 || out[0] != "@@ -1,0 +1 @@" {
		t.Fatalf("out = %q, want header @@ -1,0 +1 @@", out)
	}
	if !strings.Contains(strings.Join(out, "\n"), "+x") {
		t.Errorf("missing +x:\n%s", out)
	}
}

func TestUnifiedU0EmptyOldFile(t *testing.T) {
	out := unifiedU0("", "x\n")
	if len(out) == 0 || out[0] != "@@ -0,0 +1 @@" {
		t.Fatalf("out = %q, want header @@ -0,0 +1 @@", out)
	}
	if !strings.Contains(strings.Join(out, "\n"), "+x") {
		t.Errorf("missing +x:\n%s", out)
	}
}

func TestUnifiedU0TrailingNewlineOnly(t *testing.T) {
	out := unifiedU0("a\nb", "a\nb\n")
	if len(out) == 0 {
		t.Fatal("expected non-empty diff for trailing-newline-only change, got none")
	}
}

func TestDiffLinesFallbackWhenNoItemChanges(t *testing.T) {
	// Both parse, but only the title changed — no item events, so fall back.
	oldRaw := "# One\n\n## 🧠 Needs action\n\n- Same\n"
	newRaw := "# Two\n\n## 🧠 Needs action\n\n- Same\n"
	out := strings.Join(diffLines(oldRaw, newRaw), "\n")
	if !strings.Contains(out, "-# One") {
		t.Errorf("expected textual fallback for non-item change:\n%s", out)
	}
}
