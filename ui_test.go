package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func testModel(t *testing.T, path string) model {
	t.Helper()
	m := newModel(path, false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	return next.(model)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func manyLines(n int, tag string) string {
	var b strings.Builder
	b.WriteString("# Title\n\n")
	for i := 0; i < n; i++ {
		b.WriteString("- item ")
		b.WriteString(tag)
		b.WriteString("\n")
	}
	return b.String()
}

// Scroll position survives a reload (no jump to top).
func TestScrollPreservedAcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "REVIEW.md")
	writeFile(t, path, manyLines(100, "one"))
	m := testModel(t, path)

	m.vp.SetYOffset(37)
	writeFile(t, path, manyLines(100, "two"))
	next, _ := m.Update(fileEventMsg{})
	m = next.(model)

	if m.vp.YOffset != 37 {
		t.Errorf("YOffset = %d after reload, want 37", m.vp.YOffset)
	}
	if !strings.Contains(stripANSI(m.vp.View()), "two") {
		t.Errorf("viewport does not show reloaded content")
	}
}

// At top before reload → still at top after.
func TestTopStaysTop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "REVIEW.md")
	writeFile(t, path, manyLines(100, "one"))
	m := testModel(t, path)

	writeFile(t, path, manyLines(120, "two"))
	next, _ := m.Update(fileEventMsg{})
	m = next.(model)

	if m.vp.YOffset != 0 {
		t.Errorf("YOffset = %d, want 0", m.vp.YOffset)
	}
}

// Content shrank below the old offset → clamp, don't point past the end.
func TestOffsetClampedWhenContentShrinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "REVIEW.md")
	writeFile(t, path, manyLines(200, "one"))
	m := testModel(t, path)

	m.vp.GotoBottom()
	writeFile(t, path, manyLines(20, "two"))
	next, _ := m.Update(fileEventMsg{})
	m = next.(model)

	if m.vp.YOffset > m.vp.TotalLineCount() {
		t.Errorf("YOffset %d beyond content (%d lines)", m.vp.YOffset, m.vp.TotalLineCount())
	}
	if !strings.Contains(stripANSI(m.vp.View()), "two") {
		t.Errorf("viewport empty after shrink")
	}
}

// Missing file at startup: wait politely, then render when it appears.
func TestMissingFileThenCreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "REVIEW.md")
	m := testModel(t, path)

	if !m.fileMissing {
		t.Fatal("fileMissing = false for nonexistent file")
	}
	if !strings.Contains(stripANSI(m.vp.View()), "waiting for") {
		t.Errorf("no waiting message shown")
	}

	writeFile(t, path, "# Hello\n\n- now it exists\n")
	next, _ := m.Update(fileEventMsg{})
	m = next.(model)

	if m.fileMissing {
		t.Error("fileMissing still true after file created")
	}
	if !strings.Contains(stripANSI(m.vp.View()), "now it exists") {
		t.Errorf("viewport does not show created file")
	}
}

// g/G jump to top/bottom.
func TestTopBottomKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "REVIEW.md")
	writeFile(t, path, manyLines(200, "x"))
	m := testModel(t, path)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = next.(model)
	if !m.vp.AtBottom() {
		t.Error("G did not go to bottom")
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = next.(model)
	if m.vp.YOffset != 0 {
		t.Error("g did not go to top")
	}
}

// A live reload sets the flash, renders amber in the status bar, and clears
// on flashOffMsg. No flash when content is unchanged.
func TestReloadFlash(t *testing.T) {
	const amber = "209;154;102" // colorHeading #D19A66 as truecolor bg
	// lipgloss strips color when stdout isn't a TTY (as in tests); force
	// truecolor so the flash's amber background is actually emitted.
	lipgloss.SetColorProfile(termenv.TrueColor)

	path := filepath.Join(t.TempDir(), "SIDECAR.md")
	writeFile(t, path, manyLines(20, "one"))
	m := testModel(t, path)

	if m.flash {
		t.Fatal("flash set before any reload")
	}

	// Content changes → flash set, amber in the bar, flashOff cmd scheduled.
	writeFile(t, path, manyLines(20, "two"))
	next, cmd := m.Update(fileEventMsg{})
	m = next.(model)
	if !m.flash {
		t.Fatal("flash not set after content change")
	}
	if cmd == nil {
		t.Error("expected a flashOff command")
	}
	if !strings.Contains(m.statusBar(), amber) {
		t.Errorf("status bar not amber while flashing:\n%q", m.statusBar())
	}

	// flashOffMsg clears it.
	next, _ = m.Update(flashOffMsg{gen: m.flashGen})
	m = next.(model)
	if m.flash {
		t.Error("flash not cleared by flashOffMsg")
	}
	if strings.Contains(m.statusBar(), amber) {
		t.Error("status bar still amber after flash cleared")
	}

	// Same content again → no flash.
	next, _ = m.Update(fileEventMsg{})
	m = next.(model)
	if m.flash {
		t.Error("flash set without a content change")
	}
}

func TestUpdatePointerMarksChangedBullet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SIDECAR.md")
	writeFile(t, path, "# T\n\n- alpha\n- beta\n")
	m := testModel(t, path)

	// change one bullet
	writeFile(t, path, "# T\n\n- alpha\n- BETA\n")
	next, _ := m.Update(fileEventMsg{})
	m = next.(model)

	view := stripANSI(m.vp.View())
	if !strings.Contains(view, "▸ BETA") {
		t.Errorf("changed bullet not marked with ▸:\n%s", view)
	}
	if !m.lineFlash {
		t.Error("lineFlash should be set after a content change")
	}
}

func TestUpdatePointerInitialLoadUnmarked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SIDECAR.md")
	writeFile(t, path, "# T\n\n- alpha\n")
	m := testModel(t, path) // first load renders via WindowSizeMsg

	if strings.Contains(stripANSI(m.vp.View()), "▸") {
		t.Errorf("initial load should mark nothing:\n%s", stripANSI(m.vp.View()))
	}
}

func TestUpdatePointerFlashOffKeepsMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SIDECAR.md")
	writeFile(t, path, "# T\n\n- alpha\n")
	m := testModel(t, path)
	writeFile(t, path, "# T\n\n- ALPHA\n")
	next, _ := m.Update(fileEventMsg{})
	m = next.(model)

	// flash on → background present
	if !strings.Contains(m.vp.View(), "\x1b[48;2;") {
		t.Error("expected flash background right after change")
	}
	next, _ = m.Update(lineFlashOffMsg{gen: m.flashGen})
	m = next.(model)
	if strings.Contains(m.vp.View(), "\x1b[48;2;") {
		t.Error("flash background should clear on lineFlashOffMsg")
	}
	if !strings.Contains(stripANSI(m.vp.View()), "▸ ALPHA") {
		t.Error("▸ marker should persist after flash clears")
	}
}

func TestUpdatePointerOverlappingFlashNotCancelled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SIDECAR.md")
	writeFile(t, path, "# T\n\n- alpha\n")
	m := testModel(t, path)

	// First change → flash, generation g1.
	writeFile(t, path, "# T\n\n- ALPHA\n")
	next, _ := m.Update(fileEventMsg{})
	m = next.(model)
	g1 := m.flashGen

	// Second change before the first timer fires → flash, generation g2 > g1.
	writeFile(t, path, "# T\n\n- ALPHA\n- beta\n")
	next, _ = m.Update(fileEventMsg{})
	m = next.(model)
	if m.flashGen == g1 {
		t.Fatal("second change should bump the flash generation")
	}

	// The FIRST timer now fires (stale gen). It must NOT clear the second flash.
	next, _ = m.Update(lineFlashOffMsg{gen: g1})
	m = next.(model)
	if !m.lineFlash {
		t.Error("a stale flash-off must not cancel the newer flash")
	}

	// The current-generation timer clears it.
	next, _ = m.Update(lineFlashOffMsg{gen: m.flashGen})
	m = next.(model)
	if m.lineFlash {
		t.Error("current-generation flash-off should clear the flash")
	}
}

func TestUpdatePointerNoFlashFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SIDECAR.md")
	writeFile(t, path, "# T\n\n- alpha\n")
	m := newModel(path, true) // noFlash
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m = next.(model)
	writeFile(t, path, "# T\n\n- ALPHA\n")
	next, _ = m.Update(fileEventMsg{})
	m = next.(model)
	if strings.Contains(m.vp.View(), "\x1b[48;2;") {
		t.Error("no-flash mode should never inject a background")
	}
	if !strings.Contains(stripANSI(m.vp.View()), "▸ ALPHA") {
		t.Error("▸ marker should still work with --no-flash")
	}
}

// A file deleted while the line flash is pending must not have its stale
// content recomposed over the "waiting for file" placeholder.
func TestUpdatePointerFileMissingDuringFlash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SIDECAR.md")
	writeFile(t, path, "# T\n\n- alpha\n")
	m := testModel(t, path)

	// A change starts the flash.
	writeFile(t, path, "# T\n\n- ALPHA\n")
	next, _ := m.Update(fileEventMsg{})
	m = next.(model)
	if !m.lineFlash {
		t.Fatal("expected flash after change")
	}

	// File disappears before the flash timer fires.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	next, _ = m.Update(fileEventMsg{})
	m = next.(model)
	if !m.fileMissing {
		t.Fatal("expected fileMissing after delete")
	}

	// Flash timer fires now → must NOT resurrect the old document.
	next, _ = m.Update(lineFlashOffMsg{gen: m.flashGen})
	m = next.(model)
	view := stripANSI(m.vp.View())
	if strings.Contains(view, "ALPHA") {
		t.Errorf("stale content recomposed over waiting view:\n%s", view)
	}
	if !strings.Contains(view, "waiting for") {
		t.Errorf("waiting placeholder lost:\n%s", view)
	}
}

// An empty-file baseline is a legitimate prior state, distinct from "no
// baseline yet" — a change after it must still be marked.
func TestUpdatePointerEmptyBaselineThenLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SIDECAR.md")
	writeFile(t, path, "") // empty file
	m := testModel(t, path)
	// first non-empty content: this is the first real render, marks nothing
	writeFile(t, path, "# T\n")
	next, _ := m.Update(fileEventMsg{})
	m = next.(model)
	// now add a bullet — must be marked even though the prior baseline was empty
	writeFile(t, path, "# T\n\n- added\n")
	next, _ = m.Update(fileEventMsg{})
	m = next.(model)
	if !strings.Contains(stripANSI(m.vp.View()), "▸ added") {
		t.Errorf("added bullet not marked after empty-file baseline:\n%s", stripANSI(m.vp.View()))
	}
}

// r key should flash like a file event when a real change is loaded.
func TestUpdatePointerRKeyFlashesOnChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SIDECAR.md")
	writeFile(t, path, "# T\n\n- alpha\n")
	m := testModel(t, path)

	// Change on disk, then force-reload with `r` before any fileEventMsg.
	writeFile(t, path, "# T\n\n- ALPHA\n")
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = next.(model)

	if !m.lineFlash {
		t.Error("r after a real change should set the line flash")
	}
	if cmd == nil {
		t.Error("r after a change should schedule flash-off commands")
	}
	if !strings.Contains(stripANSI(m.vp.View()), "▸ ALPHA") {
		t.Errorf("r should render the change markers:\n%s", stripANSI(m.vp.View()))
	}
}

// The status bar is exactly pane width — never wider.
func TestStatusBarWidth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "REVIEW.md")
	writeFile(t, path, "# T\n")
	m := testModel(t, path)

	for _, w := range []int{30, 60, 120} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 20})
		mm := next.(model)
		if got := visibleWidth(mm.statusBar()); got != w {
			t.Errorf("status bar width %d, want %d", got, w)
		}
	}
}

// Two worktrees produce two boards with the same file name; the status bar
// carries the project directory so they read as different files.
func TestStatusBarNamesTheProjectDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "adjustmunk", sidecarDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "sidecar.md")
	writeFile(t, path, "# T\n")

	m := testModel(t, path)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	bar := stripANSI(next.(model).statusBar())
	if !strings.Contains(bar, "adjustmunk/sidecar.md") {
		t.Errorf("status bar = %q, want the project directory in the name", bar)
	}
}

// A resize before any content change must not mark anything (regression:
// hasBaseline true + empty prevBaseline diffed against the whole document).
func TestUpdatePointerResizeBeforeChangeUnmarked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SIDECAR.md")
	writeFile(t, path, "# T\n\n- alpha\n- beta\n")
	m := testModel(t, path) // first render via the initial WindowSizeMsg

	next, _ := m.Update(tea.WindowSizeMsg{Width: 50, Height: 20})
	m = next.(model)
	if strings.Contains(stripANSI(m.vp.View()), "▸") {
		t.Errorf("resize before any change should mark nothing:\n%s", stripANSI(m.vp.View()))
	}
}

// The `r` force-reload before any content change must not mark anything.
func TestUpdatePointerRKeyBeforeChangeUnmarked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SIDECAR.md")
	writeFile(t, path, "# T\n\n- alpha\n")
	m := testModel(t, path)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = next.(model)
	if strings.Contains(stripANSI(m.vp.View()), "▸") {
		t.Errorf("r before any change should mark nothing:\n%s", stripANSI(m.vp.View()))
	}
}

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

// On a headingless file (no board parsed), Space must fall through to the
// viewport and page down, restoring its pre-feature behavior instead of
// being swallowed by toggleCursor's no-op.
func TestSpaceScrollsWithoutSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, path, manyLines(200, "x"))
	m := testModel(t, path)
	m.reload(true)

	if len(m.board.Sections) != 0 {
		t.Fatalf("expected no board sections for a headingless file, got %d", len(m.board.Sections))
	}
	if m.vp.YOffset != 0 {
		t.Fatalf("YOffset = %d, want 0 before Space", m.vp.YOffset)
	}

	m = sendKey(t, m, " ")
	if m.vp.YOffset == 0 {
		t.Error("Space should have scrolled the viewport on a headingless file, but YOffset is still 0")
	}
}

// Enter on a headingless file remains a harmless no-op: no crash, no change.
func TestEnterNoOpWithoutSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, path, "# Title\n\nplain notes, no headings\n")
	m := testModel(t, path)
	m.reload(true)

	before := stripANSI(m.vp.View())
	m = sendSpecialKey(t, m, tea.KeyEnter)
	after := stripANSI(m.vp.View())

	if before != after {
		t.Errorf("Enter altered a headingless file's render:\nbefore: %q\nafter:  %q", before, after)
	}
}
