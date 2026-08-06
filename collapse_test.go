// collapse_test.go
package main

import (
	"strings"
	"testing"
)

func TestItemCountExcludesPlaceholder(t *testing.T) {
	raw := "## ✅ Done\n\n- nothing yet\n"
	b, _ := parseBoard(raw)
	if got := itemCount(b.Sections[0]); got != 0 {
		t.Errorf("itemCount = %d, want 0", got)
	}
}

func TestItemCountCountsRealItems(t *testing.T) {
	raw := "## 🚧 In progress\n\n- Fix the parser\n- Ship v2\n"
	b, _ := parseBoard(raw)
	if got := itemCount(b.Sections[0]); got != 2 {
		t.Errorf("itemCount = %d, want 2", got)
	}
}

func TestSeedDefaultsCollapsesDoneAndShipped(t *testing.T) {
	raw := "## 🧠 Needs action\n\n- x\n\n## ✅ Done\n\n- x\n\n## 📦 Shipped\n\n- x\n"
	b, _ := parseBoard(raw)
	collapsed := map[string]bool{}
	seedDefaults(b, collapsed)

	if collapsed["🧠 Needs action"] {
		t.Error("Needs action should default expanded")
	}
	if !collapsed["✅ Done"] {
		t.Error("Done should default collapsed")
	}
	if !collapsed["📦 Shipped"] {
		t.Error("Shipped should default collapsed")
	}
}

func TestSeedDefaultsDoesNotOverwriteExisting(t *testing.T) {
	raw := "## ✅ Done\n\n- x\n"
	b, _ := parseBoard(raw)
	collapsed := map[string]bool{"✅ Done": false} // user already expanded it
	seedDefaults(b, collapsed)

	if collapsed["✅ Done"] {
		t.Error("seedDefaults must not override a label already in the map")
	}
}

func TestApplyCollapseAppendsCountToEveryHeading(t *testing.T) {
	raw := "## 🧠 Needs action\n\n- Review PR #7\n\n## ✅ Done\n\n- nothing yet\n"
	b, _ := parseBoard(raw)
	out := applyCollapse(raw, b, map[string]bool{})

	if !strings.Contains(out, "## 🧠 Needs action (1)") {
		t.Errorf("missing count on Needs action:\n%s", out)
	}
	if !strings.Contains(out, "## ✅ Done (0)") {
		t.Errorf("missing (0) count on empty Done:\n%s", out)
	}
}

func TestApplyCollapseStripsCollapsedSectionBody(t *testing.T) {
	raw := "## 🧠 Needs action\n\n- Review PR #7\n\n## ✅ Done\n\n- Shipped thing\n"
	b, _ := parseBoard(raw)
	out := applyCollapse(raw, b, map[string]bool{"✅ Done": true})

	if !strings.Contains(out, "Review PR #7") {
		t.Error("expanded section's item should survive")
	}
	if strings.Contains(out, "Shipped thing") {
		t.Errorf("collapsed section's item should be stripped:\n%s", out)
	}
	if !strings.Contains(out, "## ✅ Done (1)") {
		t.Errorf("collapsed heading still needs its count:\n%s", out)
	}
}

func TestApplyCollapseKeepsContinuationLinesTogether(t *testing.T) {
	raw := "## 📦 Shipped\n\n- Release v2\n  https://example.com/v2\n\n## 🧠 Needs action\n\n- x\n"
	b, _ := parseBoard(raw)
	out := applyCollapse(raw, b, map[string]bool{"📦 Shipped": true})

	if strings.Contains(out, "Release v2") || strings.Contains(out, "example.com/v2") {
		t.Errorf("collapsed item and its continuation line should both be gone:\n%s", out)
	}
	if !strings.Contains(out, "## 🧠 Needs action (1)") || !strings.Contains(out, "- x") {
		t.Errorf("later expanded section should be untouched:\n%s", out)
	}
}

func TestApplyCollapseNoSectionsIsPassthrough(t *testing.T) {
	raw := "just some text, no headings\n"
	out := applyCollapse(raw, Board{}, map[string]bool{})
	if out != raw {
		t.Errorf("no-section board should pass through unchanged, got %q", out)
	}
}
