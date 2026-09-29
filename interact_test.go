package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func press(t *testing.T, m model, keys ...string) model {
	t.Helper()
	for _, k := range keys {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		if k == "esc" {
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		}
		next, _ := m.Update(msg)
		m = next.(model)
	}
	return m
}

func interactModel(t *testing.T) (model, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, p, editBoard)
	return testModel(t, p), p
}

func TestItemStartLinesMapEveryItem(t *testing.T) {
	m, _ := interactModel(t)
	got := 0
	for si, starts := range m.itemStarts {
		if len(starts) != len(m.board.Sections[si].Items) && !m.collapsed[m.board.Sections[si].Label] {
			t.Fatalf("section %d mapped %d of %d", si, len(starts), len(m.board.Sections[si].Items))
		}
		got += len(starts)
	}
	if got == 0 {
		t.Fatal("no items mapped")
	}
}

func TestTickItemFromKeyboard(t *testing.T) {
	m, p := interactModel(t)
	m = press(t, m, "]", "x")
	if got := readFile(t, p); !strings.Contains(got, "- [x] Ship it") {
		t.Fatalf("not ticked:\n%s", got)
	}
	m = press(t, m, "x")
	if got := readFile(t, p); !strings.Contains(got, "- [ ] Ship it") {
		t.Fatalf("not unticked:\n%s", got)
	}
}

func TestAnswerAskFromKeyboard(t *testing.T) {
	m, p := interactModel(t)
	m = press(t, m, "]", "]", "n")
	if got := readFile(t, p); !strings.Contains(got, "  Ask: yes | no | done\n  Answer: no\n") {
		t.Fatalf("not answered:\n%s", got)
	}
	press(t, m, "1")
	if got := readFile(t, p); strings.Count(got, "Answer:") != 1 || !strings.Contains(got, "Answer: yes") {
		t.Fatalf("answer not replaced:\n%s", got)
	}
}

func TestDoneKeyOnAskAnswersDoneNotMove(t *testing.T) {
	m, p := interactModel(t)
	press(t, m, "]", "]", "d")
	got := readFile(t, p)
	if !strings.Contains(got, "Answer: done") || strings.Count(got, "Pick a path") != 1 {
		t.Fatalf("d on a prompt offering done should answer it:\n%s", got)
	}
	b, _ := parseBoard(got)
	if len(b.Sections[2].Items) != 1 {
		t.Fatalf("item must not move:\n%s", got)
	}
}

func TestDoneKeyMovesPlainItem(t *testing.T) {
	m, p := interactModel(t)
	m = press(t, m, "]", "d")
	b, _ := parseBoard(readFile(t, p))
	if len(b.Sections[2].Items) != 2 || b.Sections[0].Items[0].Key != "Pick a path" {
		t.Fatalf("not moved:\n%s", readFile(t, p))
	}
	if m.itemSec != -1 {
		t.Fatal("cursor should clear after a move")
	}
}

func TestEditRefusedWhenAgentChangedItem(t *testing.T) {
	m, p := interactModel(t)
	m = press(t, m, "]")
	writeFile(t, p, strings.Replace(editBoard, "Ship it", "Ship it now", 1))
	m = press(t, m, "x")
	if !strings.Contains(m.notice, "changed") {
		t.Fatalf("notice %q", m.notice)
	}
	if got := readFile(t, p); strings.Contains(got, "[x]") {
		t.Fatalf("stale edit was written:\n%s", got)
	}
}

func TestEscClearsItemCursor(t *testing.T) {
	m, _ := interactModel(t)
	m = press(t, m, "]", "esc")
	if m.itemSec != -1 {
		t.Fatal("esc should clear")
	}
}

func TestMouseClickTicksBoxOnlyInMouseMode(t *testing.T) {
	m, p := interactModel(t)
	line := m.itemStarts[0][0]
	click := tea.MouseMsg{X: 0, Y: line - m.vp.YOffset, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	next, _ := m.Update(click)
	m = next.(model)
	if strings.Contains(readFile(t, p), "[x]") {
		t.Fatal("click must be ignored while mouse mode is off")
	}
	m = press(t, m, "M")
	next, _ = m.Update(click)
	if !strings.Contains(readFile(t, p), "- [x] Ship it") {
		t.Fatal("click on the box should tick in mouse mode")
	}
	_ = next
}

func TestEditPreservesFileMode(t *testing.T) {
	m, p := interactModel(t)
	os.Chmod(p, 0o600)
	press(t, m, "]", "x")
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
}
