# Collapsible Sections Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-section item counts and a collapse/expand toggle (with a section cursor) to the sidecar TUI viewer, so `✅ Done` and `📦 Shipped` don't push live sections off-screen.

**Architecture:** Rendering-only — the board file on disk is never written. `board.go`'s `parseBoard` gains line-range tracking so a new `applyCollapse` can rewrite the in-memory markdown (append `(N)` to headings, strip collapsed sections' bodies) before it reaches `renderMarkdown`. The model tracks a `cursor` (which section) and `collapsed` (which labels are hidden); `Tab`/`Shift+Tab` move the cursor, `Enter`/`Space` toggle collapse at the cursor.

**Tech Stack:** Go, bubbletea/bubbles (TUI), glamour (markdown→ANSI). No new dependencies.

## Global Constraints

- Rendering-only: the underlying markdown file is never modified by this feature (spec: Architecture).
- No new state file; `collapsed` and `cursor` live only in `model`, reset every launch (spec: Scope).
- Default collapse rule applies only to the exact labels `"✅ Done"` and `"📦 Shipped"`; every other label defaults expanded (spec: Default collapse rule).
- Header count is always shown, including `(0)`, and excludes the `nothing yet` placeholder (spec: Rendering).
- `Tab`/`Shift+Tab` move the section cursor (wrap at both ends); `Enter`/`Space` toggle collapse at the cursor (spec: Cursor and keybinds).
- Cursor's header line is highlighted via the existing `applyLineBg`-style background tint, not a new mechanism (spec: Highlighting the cursor's header).
- Files with no `## ` headings (`parseBoard` `ok=false`) are unaffected — collapse is a no-op passthrough (spec: Edge cases).

---

## File Structure

- **Modify `board.go`**: `BoardItem` gains `StartLine`/`EndLine` (0-indexed, inclusive, into the raw text split by `"\n"`); `BoardSection` gains `HeaderLine`. `parseBoard` fills these in as it already walks the file line-by-line — no new parsing logic, just recording indices it already has.
- **Create `collapse.go`**: pure functions operating on a parsed `Board` — `itemCount`, `seedDefaults`, `applyCollapse`. No model/UI knowledge.
- **Create `collapse_test.go`**: tests for the above.
- **Modify `diff.go`**: add `sectionHeaderLines` (locates rendered H2 lines) and `applyCursorHighlight` (tints one line). Both operate on already-rendered ANSI lines, same neighborhood as `composeMarked`/`isBulletLine`.
- **Modify `style.go`**: add `colorCursorBg` constant next to the other palette constants.
- **Modify `ui.go`**: `model` gains `board Board`, `collapsed map[string]bool`, `cursor int`, `headerLines []int`; `reload()` wires in `applyCollapse`/`seedDefaults`; new `compose()`, `moveCursor()`, `toggleCursor()`, `scrollToCursor()` methods; `Update()` handles `tab`/`shift+tab`/`enter`/`" "`.
- **Modify `main.go`**: add the two new keys to the `help` text.
- **Modify `README.md`**: add the two new keys to the keys bullet.

---

### Task 1: Track line ranges while parsing the board

**Files:**
- Modify: `board.go`
- Test: `board_test.go`

**Interfaces:**
- Produces: `BoardItem.StartLine int`, `BoardItem.EndLine int` (0-indexed, inclusive, into `strings.Split(raw, "\n")`); `BoardSection.HeaderLine int` (0-indexed index of that section's `"## "` line). Task 2 (`collapse.go`) consumes these.

- [ ] **Step 1: Write the failing test**

Add to `board_test.go`:

```go
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
```

Add `"strings"` to the test file's imports if not already present (it is — `board_test.go` currently only imports `"testing"`, so add `"strings"` too).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestParseBoardLineRanges -v`
Expected: FAIL — `BoardItem.StartLine undefined` (compile error, since the fields don't exist yet).

- [ ] **Step 3: Add the fields and fill them in**

In `board.go`, change the structs:

```go
// BoardItem is one top-level bullet plus its continuation lines. Key is the
// normalized first line — the identity items are matched by across versions.
// StartLine/EndLine are 0-indexed, inclusive line indices into the raw text
// split by "\n" — the exact lines applyCollapse (collapse.go) removes when
// this item's section is collapsed.
type BoardItem struct {
	Key       string
	Raw       string
	StartLine int
	EndLine   int
}

// BoardSection is one "## " heading and the items under it. Label is the
// heading without the "## " prefix. HeaderLine is the 0-indexed line index
// of the "## " line itself.
type BoardSection struct {
	Label      string
	Items      []BoardItem
	HeaderLine int
}
```

Change `parseBoard`'s loop to track the line index and use it. Replace:

```go
	for _, ln := range strings.Split(raw, "\n") {
```

with:

```go
	rawLines := strings.Split(raw, "\n")
	for lineNo, ln := range rawLines {
```

Then update the three places that build items/sections to record positions. The heading case becomes:

```go
		case strings.HasPrefix(ln, "## "):
			b.Sections = append(b.Sections, BoardSection{
				Label:      strings.TrimSpace(strings.TrimPrefix(ln, "## ")),
				HeaderLine: lineNo,
			})
			cur = &b.Sections[len(b.Sections)-1]
			item = nil
```

The bullet case becomes:

```go
		case strings.HasPrefix(ln, "- "):
			cur.Items = append(cur.Items, BoardItem{
				Key:       normalizeItem(ln),
				Raw:       ln,
				StartLine: lineNo,
				EndLine:   lineNo,
			})
			item = &cur.Items[len(cur.Items)-1]
```

The continuation case (`item != nil`) grows the range:

```go
		case item != nil:
			item.Raw += "\n" + ln
			item.EndLine = lineNo
```

The fence-continuation lines (two spots earlier in the function, `if item != nil { item.Raw += "\n" + ln }`) also need `item.EndLine = lineNo` added right after each — a fence line inside an open item is a continuation line too, so the range must include it. There are two such spots (the fence-toggle line and the "already inside a fence" branch); add the same statement to both.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... -run TestParseBoardLineRanges -v`
Expected: PASS. Also run the full existing board suite to confirm nothing regressed: `go test ./... -run TestParseBoard -v` — all prior tests (`TestParseBoardSectionsAndItems`, `TestParseBoardContinuationLines`, `TestParseBoardFencedCodeBlockNotParsedAsMarkup`, etc.) still pass unchanged, since only new fields were added.

- [ ] **Step 5: Commit**

```bash
git add board.go board_test.go
git commit -m "feat: track line ranges while parsing the board

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: Item counting, default-collapse seeding, and the collapse rewrite

**Files:**
- Create: `collapse.go`
- Create: `collapse_test.go`

**Interfaces:**
- Consumes: `Board`, `BoardSection`, `BoardItem` (board.go, Task 1); `emptySectionPlaceholder` (sections.go).
- Produces: `itemCount(s BoardSection) int`, `seedDefaults(board Board, collapsed map[string]bool)`, `applyCollapse(raw string, board Board, collapsed map[string]bool) string`. Task 4 (ui.go) calls all three.

- [ ] **Step 1: Write the failing tests**

Create `collapse_test.go`:

```go
// collapse_test.go
package main

import "testing"

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
```

Add `"strings"` to the imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run 'TestItemCount|TestSeedDefaults|TestApplyCollapse' -v`
Expected: FAIL — compile error, `itemCount`/`seedDefaults`/`applyCollapse` undefined.

- [ ] **Step 3: Implement collapse.go**

```go
// collapse.go — rendering-only section collapse: header item counts and
// stripping a collapsed section's body out of the markdown before it
// reaches glamour. The board file on disk is never touched.
package main

import (
	"strconv"
	"strings"
)

// itemCount is a section's item count, excluding the "nothing yet"
// placeholder every scaffolded empty section starts with (sections.go).
func itemCount(s BoardSection) int {
	n := 0
	for _, it := range s.Items {
		if it.Key == emptySectionPlaceholder {
			continue
		}
		n++
	}
	return n
}

// seedDefaults seeds collapsed[label] the first time a label is seen —
// true only for the exact labels "✅ Done" and "📦 Shipped", false for
// everything else. A label already present in the map (the user has
// toggled it, or it was seeded on a prior reload) is left untouched, so
// this is safe to call on every reload.
func seedDefaults(board Board, collapsed map[string]bool) {
	for _, s := range board.Sections {
		if _, seen := collapsed[s.Label]; seen {
			continue
		}
		collapsed[s.Label] = s.Label == "✅ Done" || s.Label == "📦 Shipped"
	}
}

// applyCollapse rewrites raw markdown: every section heading gets its item
// count appended as "(N)" (N excludes the placeholder, shown even at 0),
// and every item belonging to a collapsed section is dropped entirely —
// its bullet line and every continuation line. board must be the result of
// parseBoard(raw); when it has no sections (parseBoard returned ok=false),
// raw is returned unchanged.
func applyCollapse(raw string, board Board, collapsed map[string]bool) string {
	if len(board.Sections) == 0 {
		return raw
	}
	lines := strings.Split(raw, "\n")

	drop := make([]bool, len(lines))
	counts := make(map[int]int, len(board.Sections)) // HeaderLine -> count
	for _, s := range board.Sections {
		counts[s.HeaderLine] = itemCount(s)
		if !collapsed[s.Label] {
			continue
		}
		for _, it := range s.Items {
			for i := it.StartLine; i <= it.EndLine && i < len(lines); i++ {
				drop[i] = true
			}
		}
	}

	var b strings.Builder
	for i, ln := range lines {
		if drop[i] {
			continue
		}
		if n, ok := counts[i]; ok {
			ln += " (" + strconv.Itoa(n) + ")"
		}
		b.WriteString(ln)
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... -run 'TestItemCount|TestSeedDefaults|TestApplyCollapse' -v`
Expected: PASS, all 7 tests.

- [ ] **Step 5: Commit**

```bash
git add collapse.go collapse_test.go
git commit -m "feat: header item counts and section-collapse rewrite

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: Locate rendered section headers and highlight the cursor's

**Files:**
- Modify: `diff.go`
- Modify: `style.go`
- Test: `diff_test.go`

**Interfaces:**
- Consumes: `stripANSI` (diff.go), `visibleWidth`, `applyLineBg`, `hexToRGB` (diff.go, existing).
- Produces: `sectionHeaderLines(lines []string) []int`, `applyCursorHighlight(display string, headerLines []int, cursor int, width int) string`. Task 4 (ui.go) calls both.

- [ ] **Step 1: Write the failing tests**

Add to `diff_test.go`:

```go
func TestSectionHeaderLinesFindsH2sInOrder(t *testing.T) {
	lines := []string{
		"",
		"▍ 🧠 Needs action (1)",
		"",
		"• item",
		"",
		"▍ ✅ Done (0)",
		"",
	}
	got := sectionHeaderLines(lines)
	if len(got) != 2 || got[0] != 1 || got[1] != 5 {
		t.Errorf("sectionHeaderLines = %v, want [1 5]", got)
	}
}

func TestSectionHeaderLinesIgnoresNonHeadingLines(t *testing.T) {
	lines := []string{"▍ not a bullet but", "  ▍ indented, not a heading start"}
	got := sectionHeaderLines(lines)
	if len(got) != 1 || got[0] != 0 {
		t.Errorf("sectionHeaderLines = %v, want [0]", got)
	}
}

func TestApplyCursorHighlightTintsTheRightLine(t *testing.T) {
	display := "▍ heading one\n• item\n▍ heading two"
	out := applyCursorHighlight(display, []int{0, 2}, 1, 40)
	lines := strings.Split(out, "\n")

	if lines[0] != "▍ heading one" {
		t.Errorf("line 0 should be untouched, got %q", lines[0])
	}
	if stripANSI(lines[2]) == lines[2] {
		t.Error("cursor's header line (index 2) should carry ANSI tinting")
	}
	if !strings.Contains(lines[2], "heading two") {
		t.Errorf("tinted line lost its text: %q", lines[2])
	}
}

func TestApplyCursorHighlightNoCursorIsPassthrough(t *testing.T) {
	display := "▍ heading\n• item"
	if out := applyCursorHighlight(display, []int{0}, -1, 40); out != display {
		t.Errorf("cursor -1 should pass through unchanged, got %q", out)
	}
	if out := applyCursorHighlight(display, []int{0}, 5, 40); out != display {
		t.Errorf("out-of-range cursor should pass through unchanged, got %q", out)
	}
}
```

`diff_test.go` already imports `"strings"` and `"testing"` (used by the existing `composeMarked` tests) — no new imports needed.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run 'TestSectionHeaderLines|TestApplyCursorHighlight' -v`
Expected: FAIL — compile error, `sectionHeaderLines`/`applyCursorHighlight` undefined.

- [ ] **Step 3: Add colorCursorBg and implement**

In `style.go`, add to the palette const block (after `colorFlashLineBg`):

```go
	colorCursorBg    = "#3A3550" // section-cursor highlight, distinct from the reload flash
```

In `diff.go`, add below `isBulletLine`:

```go
// sectionHeaderLines returns the indices of lines that are a rendered H2
// heading — glamour's "▍ " prefix (style.go) — in document order. For a
// board file this lines up 1:1 with board.Sections, since every board
// section is an H2 in the same order it appears in the source.
func sectionHeaderLines(lines []string) []int {
	var idx []int
	for i, ln := range lines {
		t := strings.TrimLeft(stripANSI(ln), " ")
		if strings.HasPrefix(t, "▍ ") {
			idx = append(idx, i)
		}
	}
	return idx
}

// applyCursorHighlight tints headerLines[cursor] with colorCursorBg, the
// section the cursor is currently on. A no-op when cursor is out of range
// (including the -1 "no board parsed" case).
func applyCursorHighlight(display string, headerLines []int, cursor int, width int) string {
	if cursor < 0 || cursor >= len(headerLines) {
		return display
	}
	idx := headerLines[cursor]
	lines := strings.Split(display, "\n")
	if idx < 0 || idx >= len(lines) {
		return display
	}
	lines[idx] = applyLineBg(lines[idx], colorCursorBg, width)
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... -run 'TestSectionHeaderLines|TestApplyCursorHighlight' -v`
Expected: PASS, all 4 tests.

- [ ] **Step 5: Commit**

```bash
git add diff.go style.go diff_test.go
git commit -m "feat: locate rendered section headers, highlight the cursor's

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: Wire collapse and the cursor into the model's render pipeline

**Files:**
- Modify: `ui.go`
- Test: `ui_test.go`

**Interfaces:**
- Consumes: `parseBoard`, `Board` (board.go); `itemCount`, `seedDefaults`, `applyCollapse` (collapse.go, Task 2); `sectionHeaderLines`, `applyCursorHighlight` (diff.go, Task 3); `composeMarked`, `changedLines` (diff.go, existing); `renderMarkdown` (style.go, existing).
- Produces: `model.board`, `model.collapsed`, `model.cursor`, `model.headerLines` fields; `(m *model) compose() string`. Task 5 (keybinds) reads/writes `model.cursor`/`model.collapsed` and calls `compose()`.

This task does not add new keybinds — it only makes the render pipeline collapse-aware, with `cursor` fixed at `-1` (no highlight) until Task 5 wires movement in. That keeps this task's tests focused on rendering, not input.

- [ ] **Step 1: Write the failing tests**

Add to `ui_test.go`:

```go
// Header shows the item count even before anything is collapsed.
func TestHeaderShowsItemCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, path, "## 🧠 Needs action\n\n- Review PR #7\n\n## ✅ Done\n\n- nothing yet\n")
	m := testModel(t, path)
	m.reload(true)

	out := stripANSI(m.vp.View())
	if !strings.Contains(out, "Needs action (1)") {
		t.Errorf("want item count in heading:\n%s", out)
	}
	if !strings.Contains(out, "Done (0)") {
		t.Errorf("want (0) count on empty Done:\n%s", out)
	}
}

// ✅ Done defaults collapsed — its item shouldn't render at all.
func TestDoneDefaultsCollapsed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, path, "## 🧠 Needs action\n\n- Review PR #7\n\n## ✅ Done\n\n- Shipped thing\n")
	m := testModel(t, path)
	m.reload(true)

	out := stripANSI(m.vp.View())
	if strings.Contains(out, "Shipped thing") {
		t.Errorf("Done should default collapsed:\n%s", out)
	}
	if !strings.Contains(out, "Review PR #7") {
		t.Errorf("Needs action should default expanded:\n%s", out)
	}
}

// Non-default sections default expanded.
func TestNeedsActionDefaultsExpanded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, path, "## 🚧 In progress\n\n- Ship v2\n")
	m := testModel(t, path)
	m.reload(true)

	if !strings.Contains(stripANSI(m.vp.View()), "Ship v2") {
		t.Error("In progress should default expanded")
	}
}

// Collapse state, once toggled, survives a reload triggered by an
// unrelated edit elsewhere in the file.
func TestCollapseStateSurvivesReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, path, "## 🧠 Needs action\n\n- x\n\n## ✅ Done\n\n- old\n")
	m := testModel(t, path)
	m.reload(true)
	m.collapsed["🧠 Needs action"] = true // manually expand-collapse to test persistence path
	m.rerenderCollapse()

	writeFile(t, path, "## 🧠 Needs action\n\n- x\n\n## ✅ Done\n\n- old\n- new\n")
	m.reload(false)

	out := stripANSI(m.vp.View())
	if strings.Contains(out, "- x") {
		t.Errorf("Needs action should still be collapsed after reload:\n%s", out)
	}
}

// A file with no "## " headings is untouched by collapse machinery.
func TestNoHeadingsFileUnaffected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, path, "# Title\n\njust some notes, no sections\n")
	m := testModel(t, path)
	m.reload(true)

	if !strings.Contains(stripANSI(m.vp.View()), "just some notes") {
		t.Error("plain file should render unchanged")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run 'TestHeaderShowsItemCount|TestDoneDefaultsCollapsed|TestNeedsActionDefaultsExpanded|TestCollapseStateSurvivesReload|TestNoHeadingsFileUnaffected' -v`
Expected: FAIL — `TestCollapseStateSurvivesReload` won't compile yet (`rerenderCollapse` undefined); the others fail at runtime (no counts, nothing collapsed).

- [ ] **Step 3: Wire it into ui.go**

Add fields to `model` (after `flashGen int`):

```go
	// Section collapse — rendering-only, never written back to the file.
	// board is the last successful parseBoard result; collapsed is keyed by
	// BoardSection.Label and persists in-memory across reloads within a run
	// (see seedDefaults, collapse.go). cursor is which section Tab/Shift+Tab
	// is on, -1 meaning "no board parsed" (headerLines is then empty too, so
	// applyCursorHighlight is a no-op regardless). headerLines maps section
	// index to its rendered line index (sectionHeaderLines, diff.go).
	board       Board
	collapsed   map[string]bool
	cursor      int
	headerLines []int
```

Change `newModel` to initialize the map and start with no cursor:

```go
func newModel(path string, noFlash bool) model {
	return model{path: path, noFlash: noFlash, collapsed: map[string]bool{}, cursor: -1}
}
```

Replace the entire `reload` function in `ui.go` with this — it is the same function with board/collapse bookkeeping added, `raw` swapped for `displayRaw` going into `renderMarkdown` (both the content render and the baseline render), `m.headerLines` set, and the final compose call routed through the new shared `compose()` method (added next):

```go
func (m *model) reload(force bool) (changed bool) {
	if !m.ready {
		return false
	}
	data, err := os.ReadFile(m.path)
	if err != nil {
		m.fileMissing = os.IsNotExist(err)
		m.loadErr = err
		m.lastMod, m.lastSize = time.Time{}, 0
		m.raw = ""
		if m.fileMissing {
			m.vp.SetContent(waitingView(m.path))
		} else {
			m.vp.SetContent(fmt.Sprintf("\n  Error reading %s:\n  %v", m.path, err))
		}
		m.vp.GotoTop()
		m.renderedLines = nil
		m.changed = nil
		m.lineFlash = false
		m.hasBaseline = false
		return false
	}
	if st, err := os.Stat(m.path); err == nil {
		m.lastMod, m.lastSize = st.ModTime(), st.Size()
	}
	m.fileMissing = false
	m.loadErr = nil

	raw := string(data)
	contentChanged := raw != m.raw
	if !force && !contentChanged {
		return false
	}
	if contentChanged {
		m.prevBaseline = m.raw // old content ("" on first load → no markers)
	}
	m.raw = raw

	// First successful render — and after an error/missing-file recovery, where
	// hasBaseline was reset — seed the baseline to the content itself, so a
	// forced re-render (resize, r) before any real change diffs against itself
	// and marks nothing.
	if !m.hasBaseline {
		m.prevBaseline = raw
	}

	// Section collapse is rendering-only: parse the board, seed any new
	// section labels' default collapse state, clamp the cursor to the
	// current section count, then rewrite what actually reaches glamour.
	board, boardOK := parseBoard(raw)
	if boardOK {
		m.board = board
		seedDefaults(board, m.collapsed)
		if m.cursor < 0 {
			m.cursor = 0
		} else if m.cursor >= len(board.Sections) {
			m.cursor = len(board.Sections) - 1
		}
	} else {
		m.board = Board{}
		m.cursor = -1
	}
	displayRaw := raw
	if boardOK {
		displayRaw = applyCollapse(raw, board, m.collapsed)
	}

	rendered, err := renderMarkdown(displayRaw, m.renderWidth())
	if err != nil {
		m.loadErr = err
		m.vp.SetContent(fmt.Sprintf("\n  Render error: %v", err))
		m.renderedLines = nil
		m.changed = nil
		m.lineFlash = false
		m.hasBaseline = false
		return false
	}
	lines := strings.Split(rendered, "\n")
	m.headerLines = sectionHeaderLines(lines)

	var changedMap map[int]bool
	if m.hasBaseline {
		// Deliberate degrade: if the baseline fails to render we show no
		// markers this pass rather than surface an error — the content render
		// above already succeeded. The baseline is rendered through the same
		// collapse rewrite, using the *current* collapsed state — collapse is
		// a live viewer setting, not a property of any one file revision.
		baseDisplay := m.prevBaseline
		if baseBoard, ok := parseBoard(m.prevBaseline); ok {
			baseDisplay = applyCollapse(m.prevBaseline, baseBoard, m.collapsed)
		}
		if base, berr := renderMarkdown(baseDisplay, m.renderWidth()); berr == nil {
			changedMap = changedLines(strings.Split(base, "\n"), lines)
		}
	}
	m.renderedLines = lines
	m.changed = changedMap

	display := m.compose()
	offset := m.vp.YOffset
	m.vp.SetContent(display)
	m.vp.SetYOffset(offset)
	m.hasBaseline = true
	return contentChanged
}
```

Add the shared `compose` method (near `recompose`):

```go
// compose renders the current cached lines through both overlays — the
// changed/flash marking (composeMarked) and the section-cursor highlight
// (applyCursorHighlight) — at the current flash and cursor state.
func (m *model) compose() string {
	display := composeMarked(m.renderedLines, m.changed, m.lineFlash && !m.noFlash, m.renderWidth())
	return applyCursorHighlight(display, m.headerLines, m.cursor, m.renderWidth())
}
```

Update `recompose` to use it:

```go
func (m *model) recompose() {
	if m.renderedLines == nil || m.fileMissing || m.loadErr != nil {
		return
	}
	display := m.compose()
	offset := m.vp.YOffset
	m.vp.SetContent(display)
	m.vp.SetYOffset(offset)
}
```

Add `rerenderCollapse`, used when the cursor toggles a section (Task 5 calls it; this task defines it and tests it directly since Task 5's keybinds aren't wired yet):

```go
// rerenderCollapse re-renders m.raw with the current m.collapsed state,
// without touching the baseline or triggering the reload flash — toggling
// a section isn't a "the file changed on disk" event. A no-op before the
// first successful load.
func (m *model) rerenderCollapse() {
	if !m.ready || m.fileMissing || m.loadErr != nil || len(m.board.Sections) == 0 {
		return
	}
	displayRaw := applyCollapse(m.raw, m.board, m.collapsed)
	rendered, err := renderMarkdown(displayRaw, m.renderWidth())
	if err != nil {
		return // m.raw already rendered fine on the last successful reload
	}
	m.renderedLines = strings.Split(rendered, "\n")
	m.headerLines = sectionHeaderLines(m.renderedLines)
	m.changed = nil
	offset := m.vp.YOffset
	m.vp.SetContent(m.compose())
	m.vp.SetYOffset(offset)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... -v`
Expected: PASS — the 5 new tests, plus the full existing suite (no regressions; `composeMarked`/`changedLines` signatures are untouched, only their caller changed).

- [ ] **Step 5: Commit**

```bash
git add ui.go ui_test.go
git commit -m "feat: wire section collapse into the render pipeline

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: Tab/Shift+Tab cursor movement, Enter/Space toggle, docs

**Files:**
- Modify: `ui.go`
- Modify: `main.go`
- Modify: `README.md`
- Test: `ui_test.go`

**Interfaces:**
- Consumes: `model.board`, `model.cursor`, `model.collapsed`, `model.headerLines` (ui.go, Task 4); `(m *model) recompose()`, `(m *model) rerenderCollapse()` (ui.go, Task 4).
- Produces: `(m *model) moveCursor(delta int)`, `(m *model) toggleCursor()`, `(m *model) scrollToCursor()`. Terminal task — nothing downstream consumes these.

- [ ] **Step 1: Write the failing tests**

Add to `ui_test.go`:

```go
func sendKey(t *testing.T, m model, key string) model {
	t.Helper()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	if next2, ok := next.(model); ok {
		return next2
	}
	t.Fatalf("Update did not return a model for key %q", key)
	return model{}
}

func sendSpecialKey(t *testing.T, m model, kt tea.KeyType) model {
	t.Helper()
	next, _ := m.Update(tea.KeyMsg{Type: kt})
	return next.(model)
}

// Tab moves the cursor forward through sections and wraps at the end.
func TestTabMovesCursorAndWraps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, path, "## 🧠 Needs action\n\n- x\n\n## 🚧 In progress\n\n- y\n")
	m := testModel(t, path)
	m.reload(true)

	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want 0 after first load", m.cursor)
	}
	m = sendSpecialKey(t, m, tea.KeyTab)
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1 after one Tab", m.cursor)
	}
	m = sendSpecialKey(t, m, tea.KeyTab)
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 after wrapping", m.cursor)
	}
}

// Shift+Tab moves backward and wraps the other way.
func TestShiftTabMovesBackwardAndWraps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, path, "## 🧠 Needs action\n\n- x\n\n## 🚧 In progress\n\n- y\n")
	m := testModel(t, path)
	m.reload(true)

	m = sendSpecialKey(t, m, tea.KeyShiftTab)
	if m.cursor != 1 {
		t.Errorf("cursor = %d, want 1 after wrapping backward", m.cursor)
	}
}

// Enter toggles the section at the cursor.
func TestEnterTogglesCollapseAtCursor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, path, "## 🚧 In progress\n\n- Ship v2\n")
	m := testModel(t, path)
	m.reload(true) // cursor starts at 0: "🚧 In progress"

	m = sendSpecialKey(t, m, tea.KeyEnter)
	if strings.Contains(stripANSI(m.vp.View()), "Ship v2") {
		t.Error("Enter should have collapsed the section under the cursor")
	}

	m = sendSpecialKey(t, m, tea.KeyEnter)
	if !strings.Contains(stripANSI(m.vp.View()), "Ship v2") {
		t.Error("second Enter should expand it again")
	}
}

// Space does the same as Enter.
func TestSpaceTogglesCollapseAtCursor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, path, "## 🚧 In progress\n\n- Ship v2\n")
	m := testModel(t, path)
	m.reload(true)

	m = sendKey(t, m, " ")
	if strings.Contains(stripANSI(m.vp.View()), "Ship v2") {
		t.Error("Space should have collapsed the section under the cursor")
	}
}

// Tab/Enter on a file with no sections is a harmless no-op.
func TestTabAndEnterNoOpWithoutSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, path, "# Title\n\nplain notes, no headings\n")
	m := testModel(t, path)
	m.reload(true)

	m = sendSpecialKey(t, m, tea.KeyTab)
	m = sendSpecialKey(t, m, tea.KeyEnter)
	if !strings.Contains(stripANSI(m.vp.View()), "plain notes") {
		t.Error("Tab/Enter should not have altered a headingless file's render")
	}
}
```

`ui_test.go` already imports `tea "github.com/charmbracelet/bubbletea"` — no new imports needed.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run 'TestTabMovesCursor|TestShiftTabMoves|TestEnterToggles|TestSpaceToggles|TestTabAndEnterNoOp' -v`
Expected: FAIL — Tab/Shift+Tab/Enter/Space currently fall through to `m.vp.Update(msg)` and do nothing to `m.cursor`/`m.collapsed`.

- [ ] **Step 3: Add the keybinds**

In `ui.go`'s `Update`, inside the `tea.KeyMsg` switch (after the existing `"G", "end"` case), add:

```go
		case "tab":
			m.moveCursor(1)
			return m, nil
		case "shift+tab":
			m.moveCursor(-1)
			return m, nil
		case "enter", " ":
			m.toggleCursor()
			return m, nil
```

Add the three new methods near `scrollToCursor`'s natural home — after `recompose`:

```go
// moveCursor steps the section cursor by delta (±1), wrapping at both
// ends, then re-tints the highlighted header and scrolls it into view. A
// no-op when no board is parsed (cursor stays -1, see reload).
func (m *model) moveCursor(delta int) {
	n := len(m.board.Sections)
	if n == 0 {
		return
	}
	m.cursor = ((m.cursor+delta)%n + n) % n
	m.recompose()
	m.scrollToCursor()
}

// toggleCursor flips collapsed[label] for the section at the cursor and
// re-renders. A no-op when no board is parsed.
func (m *model) toggleCursor() {
	if len(m.board.Sections) == 0 {
		return
	}
	label := m.board.Sections[m.cursor].Label
	m.collapsed[label] = !m.collapsed[label]
	m.rerenderCollapse()
}

// scrollToCursor nudges the viewport's YOffset just enough to bring the
// cursor's header line into view, with a 1-line margin — it does not
// re-center the pane. A no-op when the cursor has no corresponding
// rendered header line yet.
func (m *model) scrollToCursor() {
	if m.cursor < 0 || m.cursor >= len(m.headerLines) {
		return
	}
	const margin = 1
	target := m.headerLines[m.cursor]
	top := m.vp.YOffset
	bottom := top + m.vp.Height - 1
	switch {
	case target < top+margin:
		m.vp.SetYOffset(max(0, target-margin))
	case target > bottom-margin:
		m.vp.SetYOffset(target - m.vp.Height + 1 + margin)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... -v`
Expected: PASS — all new tests, plus the full existing suite.

- [ ] **Step 5: Update the help text and README**

In `main.go`, change the `keys:` block in the `help` const:

```
keys:  j/k, arrows, PgUp/PgDn                scroll
       g / G                                top / bottom
       tab / shift+tab                      move between sections
       enter / space                        collapse / expand section
       r                                    force reload
       q                                    quit
```

In `README.md`, extend the existing keys bullet (find the line starting `- Scrolls with...`) to add a second bullet right after it:

```
- `Tab` and `Shift+Tab` move a cursor between sections; `Enter` or `Space` collapses or expands the section under it. `✅ Done` and `📦 Shipped` start collapsed — every heading shows its item count, e.g. `✅ Done (12)`.
```

- [ ] **Step 6: Run the full suite once more and commit**

```bash
go test ./...
git add ui.go main.go README.md ui_test.go
git commit -m "feat: Tab/Shift+Tab section cursor, Enter/Space to collapse

Closes #6.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Manual smoke test (after Task 5)

1. `go build -o /tmp/sidecar .`
2. `printf '## 🧠 Needs action\n\n- Try it\n\n## ✅ Done\n\n- Old thing\n- Older thing\n' > /tmp/smoke.md`
3. `/tmp/sidecar /tmp/smoke.md`
4. Confirm: `✅ Done (2)` shows collapsed (no bullets under it) on open; `🧠 Needs action (1)` shows expanded with its highlighted header (cursor starts there).
5. Press `Tab` — highlight moves to `✅ Done`. Press `Enter` — its two items appear, header still reads `(2)`. Press `Space` — collapses again.
6. Press `q` to quit.
