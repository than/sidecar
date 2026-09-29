package main

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

const marksBoard = `# Board

## 🧠 Needs you

- Config rewritten — the deploy script now reads the new keys.
  Ask: What did you change on your side in the meantime?
  Next: reply

## 🤖 Agent queue

- Second item.

## ✅ Done

- old
`

func marksModel(t *testing.T) (model, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, p, marksBoard)
	m := testModel(t, p)
	m.mouse = true
	return m, p
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

func lineWith(m model, text string) (int, string) {
	for i, ln := range m.renderedLines {
		if plain := stripANSI(ln); strings.Contains(plain, text) {
			return i, plain
		}
	}
	return -1, ""
}

func mouseAt(t *testing.T, m model, text string, x int, action tea.MouseAction, b tea.MouseButton) model {
	t.Helper()
	i, plain := lineWith(m, text)
	if i < 0 {
		t.Fatalf("no rendered line contains %q", text)
	}
	col := visibleWidth(plain[:strings.Index(plain, text)]) + x
	next, _ := m.Update(tea.MouseMsg{X: col, Y: i - m.vp.YOffset, Action: action, Button: b})
	return next.(model)
}

func clickAt(t *testing.T, m model, text string, x int) model {
	return mouseAt(t, m, text, x, tea.MouseActionPress, tea.MouseButtonLeft)
}

func TestAskAndAnswerLinesAreRedrawn(t *testing.T) {
	raw := marksBoard + ""
	raw = strings.Replace(raw, "  Next: reply", "  Answer: restarted the worker\n  Next: reply", 1)
	got := strings.Join(plainLines(t, raw, nil, 80), "\n")
	if !strings.Contains(got, "? What did you change on your side in the meantime?") || !strings.Contains(got, "↳ restarted the worker") {
		t.Fatalf("question/reply not drawn:\n%s", got)
	}
	if strings.Contains(got, "Ask:") || strings.Contains(got, "Answer:") {
		t.Fatalf("raw keywords leaked:\n%s", got)
	}
}

func TestNoEmojiOrButtonsAreDrawnAroundItems(t *testing.T) {
	got := strings.Join(plainLines(t, marksBoard, nil, 80), "\n")
	for _, bad := range []string{"[ ", "💬", "Reply", "Done ]"} {
		if strings.Contains(got, bad) {
			t.Fatalf("%q must not appear in the item area:\n%s", bad, got)
		}
	}
}

func TestOneBlankLineSeparatesItems(t *testing.T) {
	raw := "## 🧠 Needs you\n\n- first\n- second\n  more\n- third\n\n## ✅ Done\n\n- a\n- b\n"
	text := strings.Join(plainLines(t, raw, nil, 60), "\n")
	for _, pair := range [][2]string{{"• first", "• second"}, {"more", "• third"}, {"• a", "• b"}} {
		a, b := strings.Index(text, pair[0]), strings.Index(text, pair[1])
		if between := text[a:b]; !strings.Contains(between, "\n\n") || strings.Contains(between, "\n\n\n") {
			t.Fatalf("want exactly one blank line between %q and %q, got %q\n%s", pair[0], pair[1], between, text)
		}
	}
	if strings.Contains(text, "\n\n\n") {
		t.Fatalf("no run of blank lines anywhere:\n%s", text)
	}
}

func TestClickBulletTicksAndClickAgainUnticks(t *testing.T) {
	m, p := marksModel(t)
	m = clickAt(t, m, "• Config rewritten", 0)
	if !strings.Contains(readFile(t, p), "- [x] Config rewritten") {
		t.Fatalf("bullet click should tick:\n%s", readFile(t, p))
	}
	if v := stripANSI(m.vp.View()); !strings.Contains(v, "✓ Config rewritten") {
		t.Fatalf("the bullet should now draw as ✓:\n%s", v)
	}
	m = clickAt(t, m, "✓ Config rewritten", 1)
	if got := readFile(t, p); strings.Contains(got, "[x]") || !strings.Contains(got, "- Config rewritten") {
		t.Fatalf("second click should untick to a plain bullet:\n%s", got)
	}
}

func TestClickQuestionOpensInlineReply(t *testing.T) {
	m, p := marksModel(t)
	m = clickAt(t, m, "? What did you change", 4)
	if !m.typing {
		t.Fatal("clicking the question should open the reply line")
	}
	m = typeText(t, m, "Restarted the worker by hand")
	if v := stripANSI(m.vp.View()); !strings.Contains(v, "↳ Restarted the worker by hand▌") || !strings.Contains(v, "⏎ send") {
		t.Fatalf("reply should be typed under the question with a hint:\n%s", v)
	}
	special(t, m, tea.KeyEnter)
	if got := readFile(t, p); !strings.Contains(got, "  Ask: What did you change on your side in the meantime?\n  Answer: Restarted the worker by hand\n") {
		t.Fatalf("reply not written under the question:\n%s", got)
	}
}

func TestEmptyReplyLineSaysWhatToDo(t *testing.T) {
	m, _ := marksModel(t)
	m = clickAt(t, m, "? What did you change", 4)
	if v := stripANSI(m.vp.View()); !strings.Contains(v, "type your reply…") {
		t.Fatalf("placeholder missing:\n%s", v)
	}
	m = special(t, m, tea.KeyEsc)
	if v := stripANSI(m.vp.View()); strings.Contains(v, "type your reply") || strings.Contains(v, "↳") {
		t.Fatalf("esc should remove the reply line:\n%s", v)
	}
}

func TestReplyOnItemWithoutQuestionIsInsertedAfterIt(t *testing.T) {
	m, p := marksModel(t)
	m = press(t, m, "]", "]", "a") // "Second item."
	m = typeText(t, m, "unprompted note")
	var rows []string
	for _, l := range strings.Split(stripANSI(m.vp.View()), "\n") {
		rows = append(rows, strings.TrimRight(l, " "))
	}
	v := strings.Join(rows, "\n")
	if !strings.Contains(v, "• Second item.\n   ↳ unprompted note▌") {
		t.Fatalf("typed line should appear under the item:\n%s", v)
	}
	special(t, m, tea.KeyEnter)
	if got := readFile(t, p); !strings.Contains(got, "- Second item.\n  Answer: unprompted note\n") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestClickElsewhereOnAnItemOnlySelectsAndExplains(t *testing.T) {
	m, p := marksModel(t)
	m = clickAt(t, m, "Next: reply", 0)
	if m.itemSec < 0 || readFile(t, p) != marksBoard || !strings.Contains(m.notice, "click its bullet") {
		t.Fatalf("a click off the controls should select and explain, notice %q", m.notice)
	}
}

func TestClickIgnoredWhenMouseIsOff(t *testing.T) {
	m, p := marksModel(t)
	m.mouse = false
	clickAt(t, m, "• Config rewritten", 0)
	if readFile(t, p) != marksBoard {
		t.Fatal("clicks must not act while mouse is off")
	}
}

func TestNothingMovesBetweenSections(t *testing.T) {
	m, p := marksModel(t)
	m = clickAt(t, m, "• Config rewritten", 0)
	m = clickAt(t, m, "? What did you change", 4)
	m = typeText(t, m, "ok")
	special(t, m, tea.KeyEnter)
	b, _ := parseBoard(readFile(t, p))
	if len(b.Sections[0].Items) != 1 || len(b.Sections[1].Items) != 1 || len(b.Sections[2].Items) != 1 {
		t.Fatalf("the human never files items:\n%s", readFile(t, p))
	}
}

func TestHoverPaintsBulletAndQuestion(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	m, _ := marksModel(t)
	m = press(t, m, "M", "M") // toggle off and back on: hover state resets cleanly
	rest := m.vp.View()

	m = mouseAt(t, m, "• Config rewritten", 0, tea.MouseActionMotion, tea.MouseButtonNone)
	if m.hover.kind != hoverBullet || m.vp.View() == rest || !strings.Contains(m.vp.View(), bulletHotStyle.Render("• Co")) {
		t.Fatalf("hovering the bullet should paint it solid (hover %+v)", m.hover)
	}
	m = mouseAt(t, m, "? What did you change", 4, tea.MouseActionMotion, tea.MouseButtonNone)
	if m.hover.kind != hoverQuestion || !strings.Contains(m.vp.View(), questionHotStyle.Render("  ? What did you change on your side in the meantime?")) {
		t.Fatalf("hovering the question should underline it (hover %+v)", m.hover)
	}
	m = mouseAt(t, m, "Next: reply", 0, tea.MouseActionMotion, tea.MouseButtonNone)
	if m.hover.line != -1 || m.vp.View() != rest {
		t.Fatal("moving off the controls should return everything to rest")
	}
}

func TestHoverIgnoredWithMouseOff(t *testing.T) {
	m, _ := marksModel(t)
	m.mouse = false
	m = mouseAt(t, m, "• Config rewritten", 0, tea.MouseActionMotion, tea.MouseButtonNone)
	if m.hover.line != -1 {
		t.Fatal("no hover while mouse is off")
	}
}

func TestUndoRestoresTheBoardAndRefusesAfterAgentEdits(t *testing.T) {
	m, p := marksModel(t)
	m = clickAt(t, m, "• Config rewritten", 0)
	if !strings.Contains(m.notice, "u to undo") {
		t.Fatalf("notice %q should offer undo", m.notice)
	}
	m = press(t, m, "u")
	if readFile(t, p) != marksBoard || !strings.HasPrefix(m.notice, "undone") {
		t.Fatalf("undo did not restore the board (notice %q):\n%s", m.notice, readFile(t, p))
	}
	m = clickAt(t, m, "• Config rewritten", 0)
	writeFile(t, p, readFile(t, p)+"- agent added this\n")
	m = press(t, m, "u")
	if !strings.Contains(readFile(t, p), "- agent added this") || !strings.Contains(m.notice, "changed since") {
		t.Fatalf("undo overwrote the agent's edit (notice %q)", m.notice)
	}
}

func TestClickingTheStatusMessageUndoes(t *testing.T) {
	m, p := marksModel(t)
	m = clickAt(t, m, "• Config rewritten", 0)
	m.Update(tea.MouseMsg{X: 5, Y: m.vp.Height, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if readFile(t, p) != marksBoard {
		t.Fatal("clicking the status bar should undo the last change")
	}
}

func TestUKeyFallsThroughWhenNothingToUndo(t *testing.T) {
	m, _ := marksModel(t)
	if m = press(t, m, "u"); m.notice != "" {
		t.Fatalf("u with nothing to undo should scroll, got notice %q", m.notice)
	}
}

const wideBoard = `# Board

## 🧠 Needs you

- PR #30 lets the human answer the agent from the board, with a long line that wraps at every pane width the test tries. It ends mid sentence so nothing hides in slack space.
  https://github.com/than/sidecar/pull/30
  Ask: What did you change on your side while this ran, and did the deploy behave?
  Next: review and merge.

## 🚧 In progress

- Short one.
- Second short one with **bold** and a [link](https://example.com/a/long/path/that/keeps/going).

## ✅ Done

- shipped
`

// The bullet and the question must be clickable however the pane wraps.
func TestControlsWorkAtEveryWidth(t *testing.T) {
	for w := 24; w <= 200; w += 7 {
		p := filepath.Join(t.TempDir(), "sidecar.md")
		writeFile(t, p, wideBoard)
		m := newModel(p, false)
		m.mouse = true
		next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: 80})
		m = next.(model)

		if m2 := clickAt(t, m, "• Short one.", 0); !strings.Contains(readFile(t, p), "- [x] Short one.") {
			t.Fatalf("width %d: bullet click did nothing (notice %q)", w, m2.notice)
		}
		writeFile(t, p, wideBoard)
		m3 := clickAt(t, m, "? What", 2)
		if !m3.typing {
			t.Fatalf("width %d: question click did nothing (notice %q)", w, m3.notice)
		}
	}
}

const longQuestionBoard = `# Board

## 🧠 Needs you

- Result of the turn.
  Ask: After relaunching, does ticking a bullet and answering a question feel right, or is something still missing from the flow?
  Next: review.
`

func TestQuestionIsIndentedAndOneColorOnEveryWrappedLine(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	p := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, p, longQuestionBoard)
	m := testModel(t, p) // 60 wide: the question wraps
	var block []string
	for _, ln := range m.renderedLines {
		plain := stripANSI(ln)
		if strings.HasPrefix(plain, "  ? ") || (len(block) > 0 && strings.HasPrefix(plain, "    ") && strings.TrimSpace(plain) != "") {
			if !strings.HasPrefix(ln, "\x1b") || !strings.Contains(ln, "229;192;123") {
				t.Fatalf("question line lost its color: %q", ln)
			}
			block = append(block, plain)
		}
	}
	if len(block) < 2 {
		t.Fatalf("expected the question to wrap over several lines, got %q", block)
	}
	for _, l := range block[1:] {
		if !strings.HasPrefix(l, "    ") {
			t.Fatalf("wrapped lines should hang under the text: %q", l)
		}
	}
	if !strings.Contains(strings.Join(strings.Fields(strings.Join(block, " ")), " "), "still missing from the flow?") {
		t.Fatalf("question text lost: %q", block)
	}
}

func TestClickOnAWrappedQuestionLineOpensReply(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, p, longQuestionBoard)
	m := testModel(t, p)
	m.mouse = true
	m = clickAt(t, m, "still missing", 1) // the last wrapped line
	if !m.typing {
		t.Fatalf("any line of the question should open the reply (notice %q)", m.notice)
	}
}

func TestBulletTargetIsFourCellsWide(t *testing.T) {
	for _, x := range []int{0, 1, 2, 3} {
		m, p := marksModel(t)
		clickAt(t, m, "• Config rewritten", x)
		if !strings.Contains(readFile(t, p), "[x] Config") {
			t.Fatalf("a click %d cells in should tick", x)
		}
	}
	m, p := marksModel(t)
	clickAt(t, m, "• Config rewritten", 4)
	if readFile(t, p) != marksBoard {
		t.Fatal("a click past the target must not tick")
	}
}

func TestJKMoveBetweenItemsAndScrollWhenThereAreNone(t *testing.T) {
	m, _ := marksModel(t)
	m = press(t, m, "j")
	if it, _, ok := m.selected(); !ok || !strings.HasPrefix(it.Key, "Config rewritten") {
		t.Fatalf("j should select the first item, got %+v", it)
	}
	m = press(t, m, "j")
	if it, _, _ := m.selected(); it.Key != "Second item." {
		t.Fatalf("j should move down, got %q", it.Key)
	}
	m = press(t, m, "k")
	if it, _, _ := m.selected(); !strings.HasPrefix(it.Key, "Config rewritten") {
		t.Fatalf("k should move up, got %q", it.Key)
	}
	// No items at all: j is plain scrolling.
	p := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, p, manyLines(100, "x"))
	s := testModel(t, p)
	before := s.vp.YOffset
	s = press(t, s, "j")
	if s.vp.YOffset != before+1 {
		t.Fatalf("with nothing to select j should scroll (offset %d → %d)", before, s.vp.YOffset)
	}
}

func TestSpaceTicksAndEnterReplies(t *testing.T) {
	m, p := marksModel(t)
	m = press(t, m, "j", " ")
	if !strings.Contains(readFile(t, p), "- [x] Config rewritten") {
		t.Fatalf("space should tick the selected item:\n%s", readFile(t, p))
	}
	m = special(t, m, tea.KeyEnter)
	if !m.typing {
		t.Fatal("enter should open the reply")
	}
}

func TestTabLeavesItemSelectionForSections(t *testing.T) {
	m, _ := marksModel(t)
	m = press(t, m, "j")
	m = special(t, m, tea.KeyTab)
	if m.itemSec != -1 {
		t.Fatal("tab moves between sections and drops the item selection")
	}
}
