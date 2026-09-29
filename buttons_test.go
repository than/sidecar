package main

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

const buttonBoard = `# Board

## 🧠 Needs you

- Config rewritten — the deploy script now reads the new keys.
  Ask: What did you change on your side in the meantime?
  Next: reply

## ✅ Done

- old
`

func buttonModel(t *testing.T) (model, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, p, buttonBoard)
	return testModel(t, p), p
}

func TestEveryOpenItemGetsAnActionRowBelowIt(t *testing.T) {
	raw := "## 🧠 Needs you\n\n- plain\n- has detail\n  more\n\n## ✅ Done\n\n- finished\n"
	b, _ := parseBoard(raw)
	got := buttonize(raw, b)
	row := "  `[ ✅ Done ]` `[ 💬 Reply ]`"
	if strings.Count(got, row) != 2 {
		t.Fatalf("want a row under each of the 2 open items, none under Done:\n%s", got)
	}
	if !strings.Contains(got, "- plain\n"+row+"\n- has detail\n  more\n"+row+"\n") {
		t.Fatalf("rows must sit directly below their item:\n%s", got)
	}
}

func TestNoRowUnderDoneOrShippedOrPlaceholder(t *testing.T) {
	raw := "## ✅ Done (1)\n\n- a\n\n## 📦 Shipped\n\n- b\n\n## 🚧 In progress\n\n- " + emptySectionPlaceholder + "\n"
	b, _ := parseBoard(raw)
	if got := buttonize(raw, b); strings.Contains(got, "Reply") {
		t.Fatalf("finished and placeholder items get no buttons:\n%s", got)
	}
}

func TestNarrativeAskShowsAsTheQuestion(t *testing.T) {
	b, _ := parseBoard(buttonBoard)
	got := buttonize(buttonBoard, b)
	if !strings.Contains(got, "  💬 What did you change on your side in the meantime?\n") || strings.Contains(got, "Ask:") {
		t.Fatalf("question not shown:\n%s", got)
	}
	if askOptions(b.Sections[0].Items[0]) != nil {
		t.Fatal("a question without | has no choices")
	}
}

func TestChoiceAskStillDrawsChoiceButtons(t *testing.T) {
	raw := "## 🧠 Needs you\n\n- Merge?\n  Ask: ✅ Go | ❌ Hold\n"
	b, _ := parseBoard(raw)
	got := buttonize(raw, b)
	if !strings.Contains(got, "  `[ ✅ Go ]` `[ ❌ Hold ]`\n") {
		t.Fatalf("choices missing:\n%s", got)
	}
	raw2 := strings.Replace(raw, "Ask:", "Answer: ❌ Hold\n  Ask:", 1)
	b2, _ := parseBoard(raw2)
	if got := buttonize(raw2, b2); !strings.Contains(got, "`[ ✓ ❌ Hold ]`") {
		t.Fatalf("recorded choice not ticked:\n%s", got)
	}
}

func TestReplyButtonReadsEditOnceReplied(t *testing.T) {
	raw := "## 🧠 Needs you\n\n- x\n  Answer: did it\n"
	b, _ := parseBoard(raw)
	if got := buttonize(raw, b); !strings.Contains(got, "`[ 💬 Edit reply ]`") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestDisplayTextSurvivesCollapseCounts(t *testing.T) {
	m, _ := buttonModel(t)
	got := displayText(m.raw, m.board, map[string]bool{"✅ Done": true})
	if !strings.Contains(got, "## ✅ Done (1)") || strings.Count(got, "[ ✅ Done ]") != 1 {
		t.Fatalf("got:\n%s", got)
	}
}

func TestButtonsRenderInThePane(t *testing.T) {
	m, _ := buttonModel(t)
	view := stripANSI(m.vp.View())
	for _, want := range []string{"[ ✅ Done ]", "[ 💬 Reply ]", "What did you change"} {
		if !strings.Contains(view, want) {
			t.Fatalf("pane missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Ask:") {
		t.Fatalf("raw Ask: line leaked:\n%s", view)
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

func TestClickReplyOpensPrefilledLineWithoutSelectingFirst(t *testing.T) {
	m, p := buttonModel(t)
	m.mouse = true
	m = clickAt(t, m, replyChip, 3)
	if !m.typing || m.input != "" {
		t.Fatalf("reply button should open an empty answer line, typing=%v input=%q", m.typing, m.input)
	}
	m = typeText(t, m, "Restarted the worker by hand")
	special(t, m, tea.KeyEnter)
	if got := readFile(t, p); !strings.Contains(got, "  Ask: What did you change on your side in the meantime?\n  Answer: Restarted the worker by hand\n") {
		t.Fatalf("reply not written under the question:\n%s", got)
	}
}

func TestEditReplyPrefillsTheExistingText(t *testing.T) {
	m, p := buttonModel(t)
	m.mouse = true
	m = clickAt(t, m, replyChip, 3)
	m = typeText(t, m, "first")
	m = special(t, m, tea.KeyEnter)
	if !strings.Contains(readFile(t, p), "Answer: first") {
		t.Fatal("setup: reply not written")
	}
	m = clickAt(t, m, editReplyChip, 3)
	if m.input != "first" {
		t.Fatalf("edit should prefill, got %q", m.input)
	}
	m = typeText(t, m, " and second")
	special(t, m, tea.KeyEnter)
	if got := readFile(t, p); strings.Count(got, "Answer:") != 1 || !strings.Contains(got, "Answer: first and second") {
		t.Fatalf("reply not replaced:\n%s", got)
	}
}

func TestUnpromptedReplyLandsLastOnAnItemThatAskedNothing(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, p, "## 🧠 Needs you\n\n- Create the API key\n  Next: create it\n")
	m := testModel(t, p)
	m.mouse = true
	m = clickAt(t, m, replyChip, 3)
	m = typeText(t, m, "made it, pasted in .env")
	special(t, m, tea.KeyEnter)
	if got := readFile(t, p); !strings.HasSuffix(got, "  Next: create it\n  Answer: made it, pasted in .env\n") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestClickDoneMovesItAndRecordsWhoDidIt(t *testing.T) {
	m, p := buttonModel(t)
	m.mouse = true
	clickAt(t, m, doneChip, 3)
	b, _ := parseBoard(readFile(t, p))
	if len(b.Sections[0].Items) != 0 || len(b.Sections[1].Items) != 2 {
		t.Fatalf("not moved:\n%s", readFile(t, p))
	}
	if answerOf(b.Sections[1].Items[1]) != "✅ Done" {
		t.Fatalf("Done should leave a reply the hook can read:\n%s", readFile(t, p))
	}
}

func TestDoneKeepsAnExistingReply(t *testing.T) {
	m, p := buttonModel(t)
	m.mouse = true
	m = clickAt(t, m, replyChip, 3)
	m = typeText(t, m, "did it differently")
	m = special(t, m, tea.KeyEnter)
	clickAt(t, m, doneChip, 3)
	if got := readFile(t, p); !strings.Contains(got, "Answer: did it differently") || strings.Contains(got, "Answer: ✅ Done") {
		t.Fatalf("Done must not overwrite a narrative reply:\n%s", got)
	}
}

func TestOptionButtonClickRecordsTheChoice(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, p, "## 🧠 Needs you\n\n- Merge?\n  Ask: Go | Hold\n")
	m := testModel(t, p)
	m.mouse = true
	clickAt(t, m, "[ Hold ]", 2)
	if got := readFile(t, p); !strings.Contains(got, "  Answer: Hold\n") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestClickBetweenButtonsOnlySelects(t *testing.T) {
	m, p := buttonModel(t)
	m.mouse = true
	m = clickAt(t, m, "Next: reply", 0)
	if m.itemSec < 0 || readFile(t, p) != buttonBoard {
		t.Fatal("a click off the buttons should select, not write")
	}
}

func TestClickIgnoredWhenMouseIsOff(t *testing.T) {
	m, p := buttonModel(t)
	clickAt(t, m, doneChip, 3)
	if readFile(t, p) != buttonBoard {
		t.Fatal("buttons must not respond while mouse is off")
	}
}

func TestEmojiChoiceAnswersToLetterKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, p, "## 🧠 Needs you\n\n- Merge?\n  Ask: ✅ Done | ❌ No\n")
	m := testModel(t, p)
	m = press(t, m, "]", "n")
	if !strings.Contains(readFile(t, p), "Answer: ❌ No") {
		t.Fatalf("n should pick ❌ No:\n%s", readFile(t, p))
	}
}

func TestAKeyRepliesOnAnyItem(t *testing.T) {
	m, p := buttonModel(t)
	m = press(t, m, "]", "a")
	if !m.typing {
		t.Fatal("a should open the reply line on any item")
	}
	m = typeText(t, m, "ok")
	special(t, m, tea.KeyEnter)
	if !strings.Contains(readFile(t, p), "Answer: ok") {
		t.Fatalf("got:\n%s", readFile(t, p))
	}
}
