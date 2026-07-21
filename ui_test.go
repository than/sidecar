package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func testModel(t *testing.T, path string) model {
	t.Helper()
	m := newModel(path)
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
