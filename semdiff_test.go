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
