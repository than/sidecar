package main

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
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

// plainLines renders a board the way the viewer does and strips the color.
func plainLines(t *testing.T, raw string, collapsed map[string]bool, width int) []string {
	t.Helper()
	b, _ := parseBoard(raw)
	lines, err := renderBoardLines(displayText(raw, b, collapsed), b, width)
	if err != nil {
		t.Fatal(err)
	}
	for i := range lines {
		lines[i] = strings.TrimRight(stripANSI(lines[i]), " ")
	}
	return lines
}

func TestEveryOpenItemGetsAnActionRowBelowIt(t *testing.T) {
	raw := "## 🧠 Needs you\n\n- plain\n- has detail\n  more\n\n## ✅ Done\n\n- finished\n"
	got := plainLines(t, raw, nil, 60)
	var rows []int
	for i, l := range got {
		if strings.Contains(l, doneChip) {
			rows = append(rows, i)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("want a row under each of the 2 open items, none under Done:\n%s", strings.Join(got, "\n"))
	}
	if !strings.HasPrefix(got[rows[0]-1], "• plain") || !strings.HasPrefix(got[rows[1]-1], "more") {
		t.Fatalf("rows must hug the last line of their item:\n%s", strings.Join(got, "\n"))
	}
	if !strings.Contains(got[rows[0]], "[ 💬 Reply ]") {
		t.Fatalf("row %q lacks Reply", got[rows[0]])
	}
}
func TestNoRowUnderDoneOrShippedOrPlaceholder(t *testing.T) {
	raw := "## ✅ Done (1)\n\n- a\n\n## 📦 Shipped\n\n- b\n\n## 🚧 In progress\n\n- " + emptySectionPlaceholder + "\n"
	b, _ := parseBoard(raw)
	if got := (buttonize(raw, b)); strings.Contains(got, "Reply") {
		t.Fatalf("finished and placeholder items get no buttons:\n%s", got)
	}
}

func TestNarrativeAskShowsAsTheQuestion(t *testing.T) {
	b, _ := parseBoard(buttonBoard)
	got := (buttonize(buttonBoard, b))
	if !strings.Contains(got, "  💬 What did you change on your side in the meantime?\n") || strings.Contains(got, "Ask:") {
		t.Fatalf("question not shown:\n%s", got)
	}
	if askOptions(b.Sections[0].Items[0]) != nil {
		t.Fatal("a question without | has no choices")
	}
}

func TestChoiceAskStillDrawsChoiceButtons(t *testing.T) {
	raw := "## 🧠 Needs you\n\n- Merge?\n  Ask: ✅ Go | ❌ Hold\n"
	got := strings.Join(plainLines(t, raw, nil, 80), "\n")
	if !strings.Contains(got, "[ ✅ Go ]") || !strings.Contains(got, "[ ❌ Hold ]") || !strings.Contains(got, "💬 Choose:") {
		t.Fatalf("choices missing:\n%s", got)
	}
	raw2 := strings.Replace(raw, "  Ask:", "  Answer: ❌ Hold\n  Ask:", 1)
	if got := strings.Join(plainLines(t, raw2, nil, 80), "\n"); !strings.Contains(got, "[ ✓ ❌ Hold ]") {
		t.Fatalf("recorded choice not ticked:\n%s", got)
	}
}
func TestReplyButtonReadsEditOnceReplied(t *testing.T) {
	raw := "## 🧠 Needs you\n\n- x\n  Answer: did it\n"
	if got := strings.Join(plainLines(t, raw, nil, 60), "\n"); !strings.Contains(got, "[ 💬 Edit reply ]") {
		t.Fatalf("got:\n%s", got)
	}
}
func TestCollapsedSectionsKeepTheirCountAndDropTheirButtons(t *testing.T) {
	m, _ := buttonModel(t)
	got := strings.Join(plainLines(t, m.raw, map[string]bool{"✅ Done": true}, 60), "\n")
	if !strings.Contains(got, "✅ Done (1)") || strings.Count(got, doneChip) != 1 {
		t.Fatalf("got:\n%s", got)
	}
}
func TestButtonsRenderInThePane(t *testing.T) {
	m, _ := buttonModel(t)
	view := (stripANSI(m.vp.View()))
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
		if plain := (stripANSI(ln)); strings.Contains(plain, text) {
			idx := strings.Index(plain, text)
			msg := tea.MouseMsg{X: visibleWidth(plain[:idx]) + x, Y: i - m.vp.YOffset, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
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

const wideBoard = `# Board

## 🧠 Needs you

- PR #30 lets the human answer the agent from the board, with a long line that wraps at every pane width the test tries. It ends mid sentence so the button row cannot hide in slack space.
  https://github.com/than/sidecar/pull/30
  Ask: What did you change on your side while this ran, and did the deploy behave?
  Next: review and merge.

## 🚧 In progress

- Short one.
- Second short one with **bold** and a [link](https://example.com/a/long/path/that/keeps/going).

## ✅ Done

- shipped
`

// A button that wraps mid-label cannot be clicked, so at every pane width
// each open item's buttons must sit whole on one rendered line, and a click
// on them must act.
func TestButtonsStayWholeAndClickableAtEveryWidth(t *testing.T) {
	for w := 24; w <= 200; w += 7 {
		p := filepath.Join(t.TempDir(), "sidecar.md")
		writeFile(t, p, wideBoard)
		m := newModel(p, false)
		m.mouse = true
		next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 60})
		m = next.(model)
		rows := 0
		for _, ln := range m.renderedLines {
			plain := (stripANSI(ln))
			if strings.Contains(plain, doneChip) {
				rows++
				if !strings.Contains(plain, replyChip) && w >= 30 {
					t.Fatalf("width %d: Done and Reply split across lines: %q", w, plain)
				}
			}
		}
		if rows != 3 {
			t.Fatalf("width %d: want 3 whole button rows, got %d\n%s", w, rows, stripANSI(strings.Join(m.renderedLines, "\n")))
		}
		m2 := clickAt(t, m, replyChip, 3)
		if !m2.typing {
			t.Fatalf("width %d: clicking Reply did nothing", w)
		}
		m3 := clickAt(t, m, doneChip, 3)
		if m3.notice == "" && readFile(t, p) == wideBoard {
			t.Fatalf("width %d: clicking Done did nothing", w)
		}
	}
}

func motionAt(t *testing.T, m model, text string, x int) model {
	t.Helper()
	for i, ln := range m.renderedLines {
		if plain := stripANSI(ln); strings.Contains(plain, text) {
			idx := strings.Index(plain, text)
			next, _ := m.Update(tea.MouseMsg{X: visibleWidth(plain[:idx]) + x, Y: i - m.vp.YOffset, Action: tea.MouseActionMotion})
			return next.(model)
		}
	}
	t.Fatalf("no rendered line contains %q", text)
	return m
}

func TestButtonsRestQuietAndPaintUnderThePointer(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	m, _ := buttonModel(t)
	m.mouse = true
	rest := m.vp.View()
	m = motionAt(t, m, replyChip, 3)
	if m.hover.line < 0 || m.hover.chip != replyChip {
		t.Fatalf("hover not tracked: %+v", m.hover)
	}
	if hot := m.vp.View(); hot == rest || !strings.Contains(hot, buttonStyle.Render(replyChip)) {
		t.Fatal("the hovered button should repaint solid blue")
	}
	if strings.Contains(rest, buttonStyle.Render(replyChip)) || strings.Contains(rest, buttonGoStyle.Render(doneChip)) {
		t.Fatal("buttons at rest must not be painted solid")
	}
	m = motionAt(t, m, "Next:", 0) // pointer leaves the buttons
	if m.hover.line != -1 || m.vp.View() != rest {
		t.Fatal("moving off a button should return it to rest")
	}
}

func TestHoverIgnoredWithMouseOff(t *testing.T) {
	m, _ := buttonModel(t)
	m = motionAt(t, m, replyChip, 3)
	if m.hover.line != -1 {
		t.Fatal("no hover while mouse is off")
	}
}

func TestUndoRestoresTheBoardAndRefusesAfterAgentEdits(t *testing.T) {
	m, p := buttonModel(t)
	m.mouse = true
	m = clickAt(t, m, doneChip, 3)
	if !strings.Contains(m.notice, "u to undo") {
		t.Fatalf("notice %q should offer undo", m.notice)
	}
	m = press(t, m, "u")
	if readFile(t, p) != buttonBoard || m.notice != "undone" {
		t.Fatalf("undo did not restore the board (notice %q):\n%s", m.notice, readFile(t, p))
	}
	// Move again, then let the agent edit: undo must not clobber it.
	m = clickAt(t, m, doneChip, 3)
	writeFile(t, p, readFile(t, p)+"- agent added this\n")
	m = press(t, m, "u")
	if !strings.Contains(readFile(t, p), "- agent added this") || !strings.Contains(m.notice, "changed since") {
		t.Fatalf("undo overwrote the agent's edit (notice %q)", m.notice)
	}
}

func TestClickingTheStatusMessageUndoes(t *testing.T) {
	m, p := buttonModel(t)
	m.mouse = true
	m = clickAt(t, m, doneChip, 3)
	next, _ := m.Update(tea.MouseMsg{X: 5, Y: m.vp.Height, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	_ = next
	if readFile(t, p) != buttonBoard {
		t.Fatal("clicking the status bar should undo the last change")
	}
}

func TestUKeyScrollsWhenNothingToUndo(t *testing.T) {
	m, _ := buttonModel(t)
	m = press(t, m, "u")
	if m.notice != "" {
		t.Fatalf("u with nothing to undo should fall through to the viewport, got %q", m.notice)
	}
}

func TestReplyIsTypedInlineUnderTheItem(t *testing.T) {
	m, _ := buttonModel(t)
	m.mouse = true
	m = clickAt(t, m, replyChip, 3)
	m = typeText(t, m, "worker restarted")
	view := stripANSI(m.vp.View())
	if !strings.Contains(view, "💬 worker restarted▌") {
		t.Fatalf("typed text should appear under the item:\n%s", view)
	}
	if strings.Contains(view, replyChip) {
		t.Fatal("the typed line replaces the button row while typing")
	}
	m = special(t, m, tea.KeyEsc)
	if v := stripANSI(m.vp.View()); !strings.Contains(v, replyChip) || strings.Contains(v, "worker restarted") {
		t.Fatalf("esc should bring the buttons back:\n%s", v)
	}
}

func TestOneBlankLineSeparatesItems(t *testing.T) {
	raw := "## 🧠 Needs you\n\n- first\n- second\n  more\n- third\n\n## ✅ Done\n\n- a\n- b\n"
	got := plainLines(t, raw, nil, 60)
	text := strings.Join(got, "\n")
	for _, pair := range [][2]string{{"• first", "• second"}, {"more", "• third"}} {
		a, b := strings.Index(text, pair[0]), strings.Index(text, pair[1])
		between := text[a:b]
		if !strings.Contains(between, "\n\n") || strings.Contains(between, "\n\n\n") {
			t.Fatalf("want exactly one blank line between %q and %q, got %q\n%s", pair[0], pair[1], between, text)
		}
	}
	if strings.Contains(text, "\n\n\n") {
		t.Fatalf("no run of blank lines anywhere:\n%s", text)
	}
	// Finished items are spaced too.
	if a, b := strings.Index(text, "• a"), strings.Index(text, "• b"); !strings.Contains(text[a:b], "\n\n") {
		t.Fatalf("Done items should be spaced:\n%s", text)
	}
}

func TestSpacerDoesNotBreakClickingOrSelection(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, p, "## 🧠 Needs you\n\n- first\n- second\n\n## ✅ Done\n\n- old\n")
	m := testModel(t, p)
	m.mouse = true
	m = clickAt(t, m, doneChip, 3) // first row's Done
	b, _ := parseBoard(readFile(t, p))
	if len(b.Sections[0].Items) != 1 || b.Sections[0].Items[0].Key != "second" {
		t.Fatalf("clicked the wrong item's Done:\n%s", readFile(t, p))
	}
	m = press(t, m, "]", "]") // items still map one-to-one with spacers present
	if it, _, ok := m.selected(); !ok || it.Key != "second" {
		t.Fatalf("selection lost with spacers in the layout")
	}
}

func TestButtonsHaveNoBackgroundAtRest(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	for _, c := range []string{doneChip, replyChip, chipText("Go", true), chipText("Go", false)} {
		if strings.Contains(styleChip(c), "48;") {
			t.Fatalf("%q paints a background at rest: %q", c, styleChip(c))
		}
	}
	if !strings.Contains(hotChip(replyChip), "48;") || !strings.Contains(hotChip(doneChip), "48;") {
		t.Fatal("hovered buttons should paint a background")
	}
}

func TestTypingRowHintsHowToSend(t *testing.T) {
	m, _ := buttonModel(t)
	m.mouse = true
	m = clickAt(t, m, replyChip, 3)
	if v := stripANSI(m.vp.View()); !strings.Contains(v, "type your reply…") || !strings.Contains(v, "⏎ send") {
		t.Fatalf("empty reply line should say what to do:\n%s", v)
	}
	m = typeText(t, m, "hello")
	if v := stripANSI(m.vp.View()); strings.Contains(v, "type your reply…") || !strings.Contains(v, "💬 hello▌") || !strings.Contains(v, "esc cancel") {
		t.Fatalf("typed reply should replace the placeholder and keep the hint:\n%s", v)
	}
}
