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

func typeText(t *testing.T, m model, s string) model {
	t.Helper()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	return next.(model)
}

func special(t *testing.T, m model, k tea.KeyType) model {
	t.Helper()
	next, _ := m.Update(tea.KeyMsg{Type: k})
	return next.(model)
}

func interactModel(t *testing.T) (model, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, p, editBoard)
	return testModel(t, p), p
}

func TestItemStartLinesMapEveryOpenItem(t *testing.T) {
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

func TestTickFromKeyboardBothWays(t *testing.T) {
	m, p := interactModel(t)
	m = press(t, m, "]", "x")
	if !strings.Contains(readFile(t, p), "- [x] Ship it") {
		t.Fatalf("not ticked:\n%s", readFile(t, p))
	}
	if !strings.Contains(m.notice, "ticked") {
		t.Fatalf("notice %q", m.notice)
	}
	press(t, m, "x")
	if got := readFile(t, p); strings.Contains(got, "[x]") || !strings.Contains(got, "- Ship it") {
		t.Fatalf("not unticked:\n%s", got)
	}
}

func TestReplyFromKeyboard(t *testing.T) {
	m, p := interactModel(t)
	m = press(t, m, "]", "]", "a")
	if !m.typing {
		t.Fatal("a should open the reply line")
	}
	// q, x, and ] are text while typing — they must not quit, tick, or select.
	m = typeText(t, m, "qx] nap")
	m = special(t, m, tea.KeySpace)
	m = typeText(t, m, "time")
	m = special(t, m, tea.KeyBackspace)
	m = special(t, m, tea.KeyEnter)
	got := readFile(t, p)
	if !strings.Contains(got, "  Ask: What should we call it?\n  Answer: qx] nap tim\n  Next: answer") {
		t.Fatalf("reply not written under the question:\n%s", got)
	}
	if m.typing || m.input != "" {
		t.Fatal("typing state should clear after enter")
	}
}

func TestReplyEscAbandonsAndBlankWritesNothing(t *testing.T) {
	m, p := interactModel(t)
	m = press(t, m, "]", "a")
	m = typeText(t, m, "nope")
	m = special(t, m, tea.KeyEsc)
	if m.typing || strings.Contains(readFile(t, p), "nope") {
		t.Fatal("esc must abandon the reply")
	}
	m = press(t, m, "a")
	m = typeText(t, m, "   ")
	special(t, m, tea.KeyEnter)
	if readFile(t, p) != editBoard {
		t.Fatal("a blank reply must not write")
	}
}

func TestReplyPrefillsTheCurrentReply(t *testing.T) {
	m, _ := interactModel(t)
	m = press(t, m, "]", "]", "a")
	m = typeText(t, m, "first")
	m = special(t, m, tea.KeyEnter)
	m = press(t, m, "]", "]", "a") // reselect and edit
	if m.input != "first" {
		t.Fatalf("edit should prefill, got %q", m.input)
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
	if strings.Contains(readFile(t, p), "[x]") {
		t.Fatal("stale edit was written")
	}
}

func TestViewerNeverMovesItems(t *testing.T) {
	m, p := interactModel(t)
	m.mouse = true
	press(t, m, "]", "d", "x", "a", "esc")
	got := readFile(t, p)
	b, _ := parseBoard(got)
	if len(b.Sections[0].Items) != 2 || len(b.Sections[2].Items) != 1 {
		t.Fatalf("no key or click may move an item between sections:\n%s", got)
	}
}

func TestEscClearsItemCursor(t *testing.T) {
	m, _ := interactModel(t)
	m = press(t, m, "]", "esc")
	if m.itemSec != -1 {
		t.Fatal("esc should clear")
	}
}

func TestEditPreservesFileMode(t *testing.T) {
	m, p := interactModel(t)
	os.Chmod(p, 0o600)
	press(t, m, "]", "x")
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
}

func TestCleanAnswerIsOneLine(t *testing.T) {
	if got := cleanAnswer(" a\nb\t\x1b[31mc  d "); got != "a b [31mc d" {
		t.Fatalf("%q", got)
	}
}

const questionBoard = `# Board

## 🧠 Needs you

- Which name?
  Ask: What should we call it?
  Next: answer
- Merge now?
  Ask: Anything blocking the merge?
  Next: answer
- Old question
  Ask: Fine?
  Answer: yes

## ✅ Done

- shipped
  Ask: Was it fine?
`

func questionModel(t *testing.T) (model, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, p, questionBoard)
	return testModel(t, p), p
}

func TestPendingQuestionsCountsOpenOnesOutsideDone(t *testing.T) {
	m, _ := questionModel(t)
	if got := m.pendingQuestions(); got != 2 {
		t.Fatalf("pending = %d, want 2 (answered and Done ones excluded)", got)
	}
	if bar := stripANSI(m.statusBar()); !strings.Contains(bar, "2 awaiting you") {
		t.Fatalf("status bar %q", bar)
	}
}

func TestQuestionNavigationSkipsAnsweredAndWraps(t *testing.T) {
	m, _ := questionModel(t)
	want := []string{"Which name?", "Merge now?", "Which name?"}
	for i, w := range want {
		m = press(t, m, "}")
		if it, _, _ := m.selected(); it.Key != w {
			t.Fatalf("step %d: %q, want %q", i, it.Key, w)
		}
	}
	m = press(t, m, "{")
	if it, _, _ := m.selected(); it.Key != "Merge now?" {
		t.Fatalf("back: %q", it.Key)
	}
}

func TestQuestionNavigationNoneWaiting(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, p, "## 🧠 Needs you\n\n- just a note\n")
	m := press(t, testModel(t, p), "}")
	if !strings.Contains(m.notice, "no questions") {
		t.Fatalf("notice %q", m.notice)
	}
}
