// interact.go — the viewer's item cursor: pick a board item, then tick it,
// answer its Ask: prompt, or send it to ✅ Done. Keyboard-first; mouse
// clicks work only while mouse mode (M) is on, so native text selection and
// clickable links stay the default.
package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

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
func applyItemHighlight(display string, starts [][]int, si, ii, total, width int) string {
	if si < 0 || si >= len(starts) || ii < 0 || ii >= len(starts[si]) {
		return display
	}
	lines := strings.Split(display, "\n")
	from := starts[si][ii]
	to := from + 1
	if ii+1 < len(starts[si]) {
		to = starts[si][ii+1]
	} else {
		for to < len(lines) && strings.TrimSpace(stripANSI(lines[to])) != "" {
			to++
		}
	}
	for i := from; i < to && i < len(lines); i++ {
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

// moveItemCursor steps to the next (+1) or previous (-1) selectable item,
// wrapping, and scrolls it into view.
func (m *model) moveItemCursor(delta int) {
	type pos struct{ s, i int }
	var all []pos
	cur := -1
	for si, starts := range m.itemStarts {
		for ii := range starts {
			if m.board.Sections[si].Items[ii].Key == emptySectionPlaceholder {
				continue
			}
			if si == m.itemSec && ii == m.itemIdx {
				cur = len(all)
			}
			all = append(all, pos{si, ii})
		}
	}
	if len(all) == 0 {
		return
	}
	next := 0
	switch {
	case cur >= 0:
		next = ((cur+delta)%len(all) + len(all)) % len(all)
	case delta < 0:
		next = len(all) - 1
	}
	m.itemSec, m.itemIdx = all[next].s, all[next].i
	m.cursor = m.itemSec
	m.recompose()
	m.scrollTo(m.itemStarts[m.itemSec][m.itemIdx])
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
	opts := askOptions(it)
	switch {
	case key == "esc":
		m.clearItemCursor()
		return true
	case key == "x" && isCheckbox(it):
		m.apply(label, it, replaceLines(toggleCheckbox), "")
		return true
	case key == "a":
		m.typing, m.input = true, answerOf(it)
		return true
	case len(opts) > 0 && optionForKey(key, opts) != "":
		choice := optionForKey(key, opts)
		m.apply(label, it, replaceLines(func(l []string) ([]string, error) { return setAnswer(l, choice) }), "replied "+choice)
		return true
	case key == "d":
		m.apply(label, it, doneAndMove, "moved to Done")
		m.itemSec, m.itemIdx = -1, -1
		return true
	}
	return false
}

// optionForKey picks an Ask: option: a digit selects by position; y, n, and
// d select "yes", "no", and "done" when the prompt offers them.
func optionForKey(key string, opts []string) string {
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		if n := int(key[0] - '1'); n < len(opts) {
			return opts[n]
		}
		return ""
	}
	want := map[string]string{"y": "yes", "n": "no", "d": "done"}[key]
	for _, o := range opts {
		if want != "" && normalizeOption(o) == want {
			return o
		}
	}
	return ""
}

func doneLabel(b Board) string {
	for _, s := range b.Sections {
		if strings.HasSuffix(s.Label, "Done") {
			return s.Label
		}
	}
	return "✅ Done"
}

// apply writes one edit, then reloads so the pane shows the file as it now
// is. A refused or failed write surfaces in the status bar.
func (m *model) apply(label string, it BoardItem, fn func(string, Board, int, int) (string, error), note string) {
	if err := editItem(m.path, label, it.Raw, fn); err != nil {
		m.notice = err.Error()
		if err == errBoardChanged {
			m.reload(true)
		}
		return
	}
	m.notice = note
	m.reload(true)
}

// hint is the status-bar line shown while an item is selected.
func (m model) hint() string {
	it, _, ok := m.selected()
	if !ok {
		return ""
	}
	parts := []string{"[ prev · ] next", "a reply"}
	if isCheckbox(it) {
		parts = append(parts, "x tick")
	}
	if opts := askOptions(it); len(opts) > 0 {
		var o []string
		for i, v := range opts {
			o = append(o, fmt.Sprintf("%d %s", i+1, v))
		}
		parts = append(parts, strings.Join(o, "  "))
	}
	parts = append(parts, "d done", "esc")
	if !m.mouse {
		parts = append(parts, "M mouse on")
	}
	return strings.Join(parts, " · ")
}

// mouseClick selects the item under a left click and, in the checkbox
// gutter, ticks it. Only wired while mouse mode is on.
func (m *model) mouseClick(x, y int) {
	if y < 0 || y >= m.vp.Height {
		return // the status bar row is not content
	}
	line := y + m.vp.YOffset
	for si, h := range m.headerLines {
		if h == line {
			m.cursor = si
			m.toggleCursor()
			return
		}
	}
	for si, starts := range m.itemStarts {
		for ii, start := range starts {
			end := start + 1
			if ii+1 < len(starts) {
				end = starts[ii+1]
			} else {
				for end < len(m.renderedLines) && strings.TrimSpace(stripANSI(m.renderedLines[end])) != "" {
					end++
				}
			}
			if line < start || line >= end || m.board.Sections[si].Items[ii].Key == emptySectionPlaceholder {
				continue
			}
			m.itemSec, m.itemIdx, m.cursor = si, ii, si
			m.recompose()
			it, label, _ := m.selected()
			switch kind, opt, hit := chipAt(it, m.renderedLines[line], x); {
			case hit && kind == chipReply:
				m.typing, m.input = true, answerOf(it)
			case hit && kind == chipDone:
				m.apply(label, it, doneAndMove, "moved to Done")
				m.itemSec, m.itemIdx = -1, -1
			case hit:
				m.apply(label, it, replaceLines(func(l []string) ([]string, error) { return setAnswer(l, opt) }), "replied "+opt)
			case x <= 1 && line == start && isCheckbox(it):
				m.apply(label, it, replaceLines(toggleCheckbox), "")
			}
			return
		}
	}
}

func (m *model) toggleMouse() tea.Cmd {
	m.mouse = !m.mouse
	if m.mouse {
		m.notice = "mouse on — click buttons, boxes, items, headers (M to release)"
		return tea.EnableMouseCellMotion
	}
	m.notice = "mouse off — native text selection restored (M to click again)"
	return tea.DisableMouse
}

// typeKey handles a keypress while an answer is being typed in the status
// bar: text appends, backspace deletes, enter writes the answer, esc (or
// ctrl+c) abandons it. Every key is consumed, so typing "q" or "d" into an
// answer never quits or moves anything.
func (m *model) typeKey(msg tea.KeyMsg) {
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.typing, m.input = false, ""
	case tea.KeyEnter:
		text := cleanAnswer(m.input)
		m.typing, m.input = false, ""
		if it, label, ok := m.selected(); ok && text != "" {
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
}

// pendingQuestions counts unanswered Ask: items outside the finished
// sections — what is waiting on the human.
func (m model) pendingQuestions() int {
	n := 0
	for _, s := range m.board.Sections {
		if strings.HasSuffix(s.Label, "Done") || strings.HasSuffix(s.Label, "Shipped") {
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

// moveQuestionCursor selects the next (+1) or previous (-1) unanswered
// question that is visible, wrapping, and says so when none is waiting.
func (m *model) moveQuestionCursor(delta int) {
	type pos struct{ s, i int }
	var all []pos
	cur := -1
	for si, starts := range m.itemStarts {
		for ii := range starts {
			if si == m.itemSec && ii == m.itemIdx {
				cur = len(all)
			}
			all = append(all, pos{si, ii})
		}
	}
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
		p := all[idx]
		if isUnanswered(m.board.Sections[p.s].Items[p.i]) {
			m.itemSec, m.itemIdx, m.cursor = p.s, p.i, p.s
			m.recompose()
			m.scrollTo(m.itemStarts[p.s][p.i])
			return
		}
	}
	m.notice = "no questions waiting"
}

// doneAndMove records "✅ Done" as the reply when the human has said nothing
// else — so the agent's hook can tell a human finished it — then moves the
// item to ✅ Done.
func doneAndMove(raw string, b Board, si, ii int) (string, error) {
	it := b.Sections[si].Items[ii]
	if answerOf(it) == "" {
		var err error
		raw, err = replaceLines(func(l []string) ([]string, error) { return setAnswer(l, "✅ Done") })(raw, b, si, ii)
		if err != nil {
			return "", err
		}
		nb, ok := parseBoard(raw)
		if !ok {
			return "", errBoardChanged
		}
		b = nb
	}
	return moveItem(raw, b, si, ii, doneLabel(b))
}
