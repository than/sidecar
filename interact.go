// interact.go — the viewer's channel back to the agent. Select an item, tick
// its bullet, or reply to the agent's question; each is one line written to
// the board, and the agent's hook reads it. Mouse clicks are on by default;
// every action has a key too. Nothing here moves an item between sections.
package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// undoEntry is one write made from the viewer: the file before and after, so
// undo can restore it only while the file still reads as we left it.
type undoEntry struct{ before, after string }

// itemStartLines maps each board item to the rendered line it starts on: a
// top-level "• ", "□ ", or "✓ " line at column 0 between a section's header
// and the next. A section whose rendered count disagrees with the parsed
// count (collapsed, or a shape the renderer lays out differently) maps to
// nil, so the cursor skips it rather than land on the wrong line.
func itemStartLines(lines []string, headers []int, board Board) [][]int {
	out := make([][]int, len(board.Sections))
	if len(headers) != len(board.Sections) {
		return out
	}
	for si := range board.Sections {
		end := len(lines)
		if si+1 < len(headers) {
			end = headers[si+1]
		}
		var starts []int
		for i := headers[si] + 1; i < end; i++ {
			t := stripANSI(lines[i])
			if strings.HasPrefix(t, "• ") || strings.HasPrefix(t, "□ ") || strings.HasPrefix(t, "✓ ") {
				starts = append(starts, i)
			}
		}
		if len(starts) == len(board.Sections[si].Items) {
			out[si] = starts
		}
	}
	return out
}

// applyItemHighlight tints the selected item's rendered lines.
func applyItemHighlight(display string, starts [][]int, si, ii, width int) string {
	if si < 0 || si >= len(starts) || ii < 0 || ii >= len(starts[si]) {
		return display
	}
	lines := strings.Split(display, "\n")
	end := itemEnd(lines, starts[si], ii)
	for i := starts[si][ii]; i < end && i < len(lines); i++ {
		if strings.TrimSpace(stripANSI(lines[i])) != "" {
			lines[i] = applyLineBg(lines[i], colorCursorBg, width)
		}
	}
	return strings.Join(lines, "\n")
}

func (m model) selected() (BoardItem, string, bool) {
	if m.itemSec < 0 || m.itemSec >= len(m.board.Sections) {
		return BoardItem{}, "", false
	}
	s := m.board.Sections[m.itemSec]
	if m.itemIdx < 0 || m.itemIdx >= len(s.Items) {
		return BoardItem{}, "", false
	}
	return s.Items[m.itemIdx], s.Label, true
}

// itemPositions lists every mapped, real item in reading order.
func (m model) itemPositions() (all [][2]int, cur int) {
	cur = -1
	for si, starts := range m.itemStarts {
		for ii := range starts {
			if m.board.Sections[si].Items[ii].Key == emptySectionPlaceholder {
				continue
			}
			if si == m.itemSec && ii == m.itemIdx {
				cur = len(all)
			}
			all = append(all, [2]int{si, ii})
		}
	}
	return all, cur
}

func (m *model) selectItem(si, ii int) {
	m.itemSec, m.itemIdx, m.cursor = si, ii, si
	m.recompose()
	m.scrollTo(m.itemStarts[si][ii])
}

// moveItemCursor steps to the next (+1) or previous (-1) item, wrapping.
func (m *model) moveItemCursor(delta int) bool {
	all, cur := m.itemPositions()
	if len(all) == 0 {
		return false
	}
	next := 0
	switch {
	case cur >= 0:
		next = ((cur+delta)%len(all) + len(all)) % len(all)
	case delta < 0:
		next = len(all) - 1
	}
	m.selectItem(all[next][0], all[next][1])
	return true
}

// moveQuestionCursor selects the next (+1) or previous (-1) unanswered
// question that is visible, wrapping, and says so when none is waiting.
func (m *model) moveQuestionCursor(delta int) {
	all, cur := m.itemPositions()
	n := len(all)
	for step := 1; step <= n; step++ {
		var idx int
		switch {
		case cur >= 0:
			idx = ((cur+delta*step)%n + n) % n
		case delta > 0:
			idx = step - 1
		default:
			idx = n - step
		}
		if isUnanswered(m.board.Sections[all[idx][0]].Items[all[idx][1]]) {
			m.selectItem(all[idx][0], all[idx][1])
			return
		}
	}
	m.notice = "no questions waiting"
}

func (m *model) clearItemCursor() {
	m.itemSec, m.itemIdx = -1, -1
	m.recompose()
}

// scrollTo brings a rendered line into view with a 1-line margin.
func (m *model) scrollTo(target int) {
	const margin = 1
	top, bottom := m.vp.YOffset, m.vp.YOffset+m.vp.Height-1
	switch {
	case target < top+margin:
		m.vp.SetYOffset(max(0, target-margin))
	case target > bottom-margin:
		m.vp.SetYOffset(target - m.vp.Height + 1 + margin)
	}
}

// itemKey handles a keypress while an item is selected. It reports whether
// the key was consumed.
func (m *model) itemKey(key string) bool {
	it, label, ok := m.selected()
	if !ok {
		return false
	}
	switch key {
	case "esc":
		m.clearItemCursor()
	case "x", " ":
		m.tick(label, it)
	case "a", "enter":
		m.startReply(it)
	default:
		return false
	}
	return true
}

func (m *model) tick(label string, it BoardItem) {
	note := "ticked"
	if isTicked(it) {
		note = "unticked"
	}
	m.apply(label, it, replaceLines(toggleTick), note)
}

func (m *model) startReply(it BoardItem) {
	m.typing, m.input = true, answerOf(it)
	m.recompose()
}

// typeKey handles a keypress while a reply is being typed under an item:
// text appends, backspace deletes, enter writes it, esc (or ctrl+c) abandons
// it. Every key is consumed, so typing "q" or "x" into a reply never quits or
// ticks anything.
func (m *model) typeKey(msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.typing, m.input = false, ""
	case tea.KeyEnter:
		text := cleanAnswer(m.input)
		it, label, ok := m.selected()
		m.typing, m.input = false, ""
		if ok && text != "" {
			m.apply(label, it, replaceLines(func(l []string) ([]string, error) { return setAnswer(l, text) }), "reply sent")
		}
	case tea.KeyBackspace:
		if r := []rune(m.input); len(r) > 0 {
			m.input = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		m.input += " "
	case tea.KeyRunes:
		m.input += string(msg.Runes)
	}
	m.recompose() // the reply is drawn under its item, so redraw on every key
}

// apply writes one edit, remembers it for undo, then reloads so the pane shows
// the file as it now is. A refused or failed write surfaces in the status bar.
func (m *model) apply(label string, it BoardItem, fn func(string, Board, int, int) (string, error), note string) {
	before, _ := os.ReadFile(m.path)
	if err := editItem(m.path, label, it.Raw, fn); err != nil {
		m.notice = err.Error()
		if err == errBoardChanged {
			m.reload(true)
		}
		return
	}
	after, _ := os.ReadFile(m.path)
	if string(before) != string(after) {
		m.undo = append(m.undo, undoEntry{string(before), string(after)})
		note += " — u to undo"
	}
	m.notice = note
	m.reload(true)
}

// undoLast restores the file as it was before the viewer's last write — but
// only while the file still reads exactly as that write left it, so undo can
// never overwrite something the agent wrote since.
func (m *model) undoLast() {
	if len(m.undo) == 0 {
		m.notice = "nothing to undo"
		return
	}
	e := m.undo[len(m.undo)-1]
	cur, err := os.ReadFile(m.path)
	if err != nil || string(cur) != e.after {
		m.undo = nil
		m.notice = "board changed since — nothing undone"
		return
	}
	if err := writeAtomic(m.path, e.before); err != nil {
		m.notice = err.Error()
		return
	}
	m.undo = m.undo[:len(m.undo)-1]
	m.itemSec, m.itemIdx = -1, -1
	m.notice = "undone"
	if len(m.undo) > 0 {
		m.notice += " — u to undo again"
	}
	m.reload(true)
}

// pendingQuestions counts unanswered questions outside the finished sections
// — what is waiting on the human.
func (m model) pendingQuestions() int {
	n := 0
	for _, s := range m.board.Sections {
		if finishedLabel(s.Label) {
			continue
		}
		for _, it := range s.Items {
			if isUnanswered(it) {
				n++
			}
		}
	}
	return n
}

// hint is the status-bar line shown while an item is selected.
func (m model) hint() string {
	it, _, ok := m.selected()
	if !ok {
		return ""
	}
	tick := "x tick"
	if isTicked(it) {
		tick = "x untick"
	}
	reply := "a reply"
	if answerOf(it) != "" {
		reply = "a edit reply"
	}
	return strings.Join([]string{"j k move", "space " + strings.TrimPrefix(tick, "x "), "enter " + strings.TrimPrefix(reply, "a "), "esc"}, " · ")
}

// itemAtLine finds the mapped, real item whose rendered lines include line.
func (m model) itemAtLine(line int) (si, ii, start int, ok bool) {
	for si, starts := range m.itemStarts {
		for ii, start := range starts {
			if m.board.Sections[si].Items[ii].Key == emptySectionPlaceholder {
				continue
			}
			if line >= start && line < itemEnd(m.renderedLines, starts, ii) {
				return si, ii, start, true
			}
		}
	}
	return 0, 0, 0, false
}

// targetAt classifies what is under a click or the pointer: the bullet (the
// first few columns of an item's first line) or any line of the item's
// question. last is the question's final line.
func (m model) targetAt(x, line int) (si, ii, last int, kind hoverKind) {
	si, ii, start, ok := m.itemAtLine(line)
	if !ok {
		return 0, 0, 0, hoverNone
	}
	if line == start && x < bulletCells {
		return si, ii, line, hoverBullet
	}
	it := m.board.Sections[si].Items[ii]
	if q := plainMark(askText(it)); q != "" {
		end := itemEnd(m.renderedLines, m.itemStarts[si], ii)
		if f, l, ok := markBlock(m.renderedLines, start+1, end, questionPrefix, q); ok && line >= f && line <= l {
			return si, ii, l, hoverQuestion
		}
	}
	return si, ii, line, hoverNone
}

// mouseClick acts on a left click: a section header collapses, a bullet
// ticks, a question opens a reply, and any other spot on an item selects it.
// Every outcome says so in the status bar, so a click never looks ignored.
func (m *model) mouseClick(x, y int) {
	if y == m.vp.Height && len(m.undo) > 0 {
		m.undoLast() // the status bar carries "u to undo"
		return
	}
	if y < 0 || y >= m.vp.Height {
		m.notice = fmt.Sprintf("click %d,%d — the status bar is not clickable", x, y)
		return
	}
	if m.typing {
		m.notice = "finish the reply first — enter sends, esc cancels"
		return
	}
	line := y + m.vp.YOffset
	if line < len(m.renderedLines) {
		if s, ok := linkAt(m.renderedLines[line], x); ok {
			m.openLink(s.url)
			return
		}
	}
	for si, h := range m.headerLines {
		if h == line {
			m.cursor = si
			m.toggleCursor()
			return
		}
	}
	si, ii, _, kind := m.targetAt(x, line)
	if _, _, _, ok := m.itemAtLine(line); !ok {
		m.notice = fmt.Sprintf("click %d,%d — nothing to click on that line", x, y)
		return
	}
	m.selectItem(si, ii)
	it, label, _ := m.selected()
	switch kind {
	case hoverBullet:
		m.tick(label, it)
	case hoverQuestion:
		m.startReply(it)
	default:
		m.notice = "item selected — click its bullet to tick, its ? question to reply"
	}
}

// setHover tracks what is under the pointer so it can be painted; it
// re-renders only when the target changes.
func (m *model) setHover(x, y int) {
	next := hoverTarget{line: -1}
	if y >= 0 && y < m.vp.Height && !m.typing {
		line := y + m.vp.YOffset
		if line < len(m.renderedLines) {
			if s, ok := linkAt(m.renderedLines[line], x); ok {
				next = hoverTarget{line: line, last: line, kind: hoverLink, link: s}
			}
		}
		if next.line >= 0 {
			// a link wins over the item it sits in
		} else if si, ii, last, kind := m.targetAt(x, line); kind != hoverNone {
			first := line
			if kind == hoverQuestion {
				start := m.itemStarts[si][ii]
				first, _, _ = markBlock(m.renderedLines, start+1, itemEnd(m.renderedLines, m.itemStarts[si], ii), questionPrefix, plainMark(askText(m.board.Sections[si].Items[ii])))
			}
			next = hoverTarget{line: first, last: last, kind: kind}
		}
	}
	if next != m.hover {
		m.hover = next
		m.recompose()
	}
}

// typingRow finds where the reply being typed is drawn: over the item's
// reply block when it has one (first through last), otherwise as a new line
// after its last line (insert). text is the styled row.
func (m model) typingRow(width int) (first, last int, text string, insert, ok bool) {
	if !m.typing || m.itemSec < 0 || m.itemSec >= len(m.itemStarts) || m.itemIdx >= len(m.itemStarts[m.itemSec]) {
		return 0, 0, "", false, false
	}
	ss := m.itemStarts[m.itemSec]
	end := itemEnd(m.renderedLines, ss, m.itemIdx)
	const hint = "⏎ send · esc cancel"
	showHint := width >= 44
	room := max(4, width-8)
	if showHint {
		room = max(4, width-8-visibleWidth(hint)-2)
	}
	shown := m.input
	if shown == "" {
		shown = "type your reply…"
	}
	for visibleWidth(shown) > room && shown != "" {
		shown = string([]rune(shown)[1:])
	}
	row := "  " + typingStyle.Render(" "+replyPrefix+shown+"▌ ")
	if showHint {
		row += "  " + hintStyle.Render(hint)
	}
	it := m.board.Sections[m.itemSec].Items[m.itemIdx]
	if r := plainMark(answerOf(it)); r != "" {
		if f, l, found := markBlock(m.renderedLines, ss[m.itemIdx]+1, end, replyPrefix, r); found {
			return f, l, row, false, true
		}
	}
	return end, end, row, true, true
}

func (m *model) toggleMouse() tea.Cmd {
	m.mouse = !m.mouse
	m.hover = hoverTarget{line: -1}
	if m.mouse {
		m.notice = "mouse on — click a bullet to tick, a ? question to reply (M to release)"
		return tea.EnableMouseAllMotion
	}
	m.notice = "mouse off — native text selection restored (M to click again)"
	return tea.DisableMouse
}
