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

func TestMouseClickOnStatusBarRowIgnored(t *testing.T) {
	m, p := interactModel(t)
	m = press(t, m, "M")
	m.vp.SetYOffset(0)
	// The status bar row sits at y == viewport height; it must not map to
	// content below the fold even when a checkbox starts on that line.
	m.mouseClick(0, m.vp.Height)
	if strings.Contains(readFile(t, p), "[x]") || m.itemSec != -1 {
		t.Fatal("status bar click acted on an item")
	}
}

const questionBoard = `# Board

## 🧠 Needs you

- Which name?
  Ask:
  Next: answer
- Merge now?
  Ask: yes | no
  Next: answer
- Old question
  Ask: yes | no
  Answer: yes

## ✅ Done

- shipped
  Ask: yes | no
`

func questionModel(t *testing.T) (model, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, p, questionBoard)
	return testModel(t, p), p
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

func TestFreeTextAnswer(t *testing.T) {
	m, p := questionModel(t)
	m = press(t, m, "]", "a")
	if !m.typing {
		t.Fatal("a on an Ask: item should open the answer line")
	}
	// q, d, and ] are text while typing — they must not quit, move, or select.
	m = typeText(t, m, "qd] nap")
	m = special(t, m, tea.KeySpace)
	m = typeText(t, m, "time")
	m = special(t, m, tea.KeyBackspace)
	m = special(t, m, tea.KeyEnter)
	got := readFile(t, p)
	if !strings.Contains(got, "  Ask:\n  Answer: qd] nap tim\n  Next: answer") {
		t.Fatalf("answer not written under the Ask: line:\n%s", got)
	}
	if m.typing || m.input != "" {
		t.Fatal("typing state should clear after enter")
	}
}

func TestFreeTextEscAbandons(t *testing.T) {
	m, p := questionModel(t)
	m = press(t, m, "]", "a")
	m = typeText(t, m, "nope")
	m = special(t, m, tea.KeyEsc)
	if m.typing || strings.Contains(readFile(t, p), "Answer: nope") {
		t.Fatal("esc must abandon the answer")
	}
}

func TestFreeTextEmptyWritesNothing(t *testing.T) {
	m, p := questionModel(t)
	m = press(t, m, "]", "a")
	m = typeText(t, m, "  ")
	special(t, m, tea.KeyEnter)
	if readFile(t, p) != questionBoard {
		t.Fatal("blank answer must not write")
	}
}

func TestFreeTextOnQuestionWithOptions(t *testing.T) {
	m, p := questionModel(t)
	m = press(t, m, "]", "]", "a")
	m = typeText(t, m, "yes, after lunch")
	special(t, m, tea.KeyEnter)
	if !strings.Contains(readFile(t, p), "  Answer: yes, after lunch\n") {
		t.Fatalf("custom answer on an optioned question:\n%s", readFile(t, p))
	}
}

func TestCleanAnswerIsOneLine(t *testing.T) {
	if got := cleanAnswer(" a\nb\t\x1b[31mc  d "); got != "a b [31mc d" {
		t.Fatalf("%q", got)
	}
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
	m = press(t, m, "}")
	if it, _, _ := m.selected(); it.Key != "Which name?" {
		t.Fatalf("first: %q", it.Key)
	}
	m = press(t, m, "}")
	if it, _, _ := m.selected(); it.Key != "Merge now?" {
		t.Fatalf("second: %q", it.Key)
	}
	m = press(t, m, "}")
	if it, _, _ := m.selected(); it.Key != "Which name?" {
		t.Fatalf("wrap: %q", it.Key)
	}
	m = press(t, m, "{")
	if it, _, _ := m.selected(); it.Key != "Merge now?" {
		t.Fatalf("back: %q", it.Key)
	}
}

func TestQuestionNavigationFromAnsweredItemSteps(t *testing.T) {
	m, _ := questionModel(t)
	m = press(t, m, "]", "]", "]") // the answered item
	m = press(t, m, "}")
	if it, _, _ := m.selected(); it.Key != "Which name?" {
		t.Fatalf("next from answered wraps to first question, got %q", it.Key)
	}
}

func TestQuestionNavigationNoneWaiting(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, p, "## 🧠 Needs you\n\n- just a note\n")
	m := testModel(t, p)
	m = press(t, m, "}")
	if !strings.Contains(m.notice, "no questions") {
		t.Fatalf("notice %q", m.notice)
	}
}
