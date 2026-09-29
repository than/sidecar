package main

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

const buttonBoard = `# Board

## 🧠 Needs you

- Merge now?
  Ask: ✅ Done | ❌ No
  Next: answer

## ✅ Done

- old
`

func buttonModel(t *testing.T) (model, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, p, buttonBoard)
	return testModel(t, p), p
}

func TestButtonizeKeepsLineCountAndDrawsChips(t *testing.T) {
	b, _ := parseBoard(buttonBoard)
	got := buttonize(buttonBoard, b)
	if strings.Count(got, "\n") != strings.Count(buttonBoard, "\n") {
		t.Fatal("buttonize changed the line count")
	}
	if !strings.Contains(got, "  `[ ✅ Done ]` `[ ❌ No ]` `[ ✎ ]`\n") {
		t.Fatalf("chips missing:\n%s", got)
	}
}

func TestButtonizeTicksTheChosenAnswer(t *testing.T) {
	raw := strings.Replace(buttonBoard, "  Next: answer", "  Answer: ❌ No\n  Next: answer", 1)
	b, _ := parseBoard(raw)
	if got := buttonize(raw, b); !strings.Contains(got, "`[ ✓ ❌ No ]`") || strings.Contains(got, "`[ ❌ No ]`") {
		t.Fatalf("chosen chip not ticked:\n%s", got)
	}
}

func TestOpenQuestionGetsOnlyTheWriteButton(t *testing.T) {
	raw := "## 🧠 Needs you\n\n- Name?\n  Ask:\n"
	b, _ := parseBoard(raw)
	if got := buttonize(raw, b); !strings.Contains(got, "  `[ ✎ ]`") {
		t.Fatalf("got %q", got)
	}
}

func TestButtonsRenderInThePane(t *testing.T) {
	m, _ := buttonModel(t)
	view := stripANSI(m.vp.View())
	if !strings.Contains(view, "[ ✅ Done ]") || !strings.Contains(view, "[ ❌ No ]") || strings.Contains(view, "Ask:") {
		t.Fatalf("pane:\n%s", view)
	}
}

func TestEmojiOptionsAnswerToLetterKeys(t *testing.T) {
	m, p := buttonModel(t)
	m = press(t, m, "]", "n")
	if got := readFile(t, p); !strings.Contains(got, "Answer: ❌ No") {
		t.Fatalf("n should pick ❌ No:\n%s", got)
	}
	press(t, m, "d")
	if got := readFile(t, p); !strings.Contains(got, "Answer: ✅ Done") || strings.Count(got, "Answer:") != 1 {
		t.Fatalf("d should pick ✅ Done:\n%s", got)
	}
}

func clickAt(t *testing.T, m model, text string, x int) model {
	t.Helper()
	for i, ln := range m.renderedLines {
		if idx := strings.Index(stripANSI(ln), text); idx >= 0 {
			msg := tea.MouseMsg{X: visibleWidth(stripANSI(ln)[:idx]) + x, Y: i - m.vp.YOffset, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
			next, _ := m.Update(msg)
			return next.(model)
		}
	}
	t.Fatalf("no rendered line contains %q", text)
	return m
}

func TestClickingAButtonAnswers(t *testing.T) {
	m, p := buttonModel(t)
	m = press(t, m, "M")
	clickAt(t, m, "[ ❌ No ]", 2)
	if got := readFile(t, p); !strings.Contains(got, "  Answer: ❌ No\n") {
		t.Fatalf("click did not answer:\n%s", got)
	}
}

func TestClickingTheWriteButtonOpensTyping(t *testing.T) {
	m, _ := buttonModel(t)
	m = press(t, m, "M")
	m = clickAt(t, m, freeChip, 1)
	if !m.typing {
		t.Fatal("✎ button should open the answer line")
	}
}

func TestClickBetweenButtonsOnlySelects(t *testing.T) {
	m, p := buttonModel(t)
	m = press(t, m, "M")
	m = clickAt(t, m, "Next: answer", 0)
	if m.itemSec < 0 || readFile(t, p) != buttonBoard {
		t.Fatal("a click off the buttons should select, not write")
	}
}

func TestClickIgnoredWithoutMouseMode(t *testing.T) {
	m, p := buttonModel(t)
	clickAt(t, m, "[ ❌ No ]", 2)
	if readFile(t, p) != buttonBoard {
		t.Fatal("buttons must not respond while mouse mode is off")
	}
}
