// marks.go — how the human's side of the board is drawn. The file stays plain
// markdown; at display time the viewer:
//
//   - shows an item's "Ask: …" line as "? …" in the accent color — a question
//     you can click, or answer with a;
//   - shows the "Answer: …" line as "↳ …";
//   - puts one blank line between items;
//   - paints the bullet, or the question, solid under the pointer.
//
// The bullet is the control: a click turns • into ✓ (the file gets "- [x]")
// and a second click turns it back. The viewer never moves an item between
// sections — the agent owns that; this is only the channel back to it.
package main

import (
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	questionStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#E5C07B"))
	questionHotStyle = lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.Color("#E5C07B"))
	replyLineStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#98C379"))
	bulletHotStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#101010")).Background(lipgloss.Color("#E5C07B"))
	typingStyle      = lipgloss.NewStyle().Bold(true).
				Foreground(lipgloss.Color("#101010")).Background(lipgloss.Color("#E5C07B"))
	hintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#8B93A5"))
)

const (
	questionPrefix = "? "
	replyPrefix    = "↳ "
)

// finishedLabel reports a ✅ Done or 📦 Shipped heading, with or without the
// " (N)" item count the display rewrite appends.
func finishedLabel(label string) bool {
	label = strings.TrimSpace(label)
	if i := strings.LastIndex(label, " ("); i >= 0 && strings.HasSuffix(label, ")") {
		label = label[:i]
	}
	return strings.HasSuffix(label, "Done") || strings.HasSuffix(label, "Shipped")
}

// displayText is the board as rendered: collapsed sections dropped and counts
// added, then each item's Ask: line redrawn as a question and its Answer: line
// as a reply. The line count never changes.
func displayText(raw string, board Board, collapsed map[string]bool) string {
	collapsedRaw := applyCollapse(raw, board, collapsed)
	b, ok := parseBoard(collapsedRaw)
	if !ok {
		return collapsedRaw
	}
	lines := strings.Split(collapsedRaw, "\n")
	for _, s := range b.Sections {
		for _, it := range s.Items {
			for i := it.StartLine + 1; i <= it.EndLine && i < len(lines); i++ {
				t := strings.TrimSpace(lines[i])
				indent := lines[i][:len(lines[i])-len(strings.TrimLeft(lines[i], " \t"))]
				switch {
				case strings.HasPrefix(t, "Ask:"):
					lines[i] = indent + questionPrefix + plainMark(strings.TrimSpace(strings.TrimPrefix(t, "Ask:")))
				case strings.HasPrefix(t, "Answer:"):
					lines[i] = indent + replyPrefix + plainMark(strings.TrimSpace(strings.TrimPrefix(t, "Answer:")))
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}

// renderBoardLines renders the display markdown, colors the question and
// reply lines, and spaces the items. board must be the parse of the file the
// display text came from, so items line up with the rendered bullets.
func renderBoardLines(display string, board Board, width int) ([]string, error) {
	rendered, err := renderMarkdown(display, width, true)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(rendered, "\n")
	if len(board.Sections) == 0 {
		return lines, nil
	}
	starts := itemStartLines(lines, sectionHeaderLines(lines), board)
	lines = rewriteMarks(lines, starts, board, width)
	starts = itemStartLines(lines, sectionHeaderLines(lines), board) // the reflow can change line counts
	return spaceItems(lines, starts), nil
}

// plainMark is question or reply text as glamour will show it: the markdown
// emphasis and code marks are dropped, so the words can be found again in the
// rendered lines.
func plainMark(s string) string { return strings.NewReplacer("`", "", "*", "").Replace(s) }

// markBlock finds the rendered lines of a question or reply inside an item's
// lines [from, to): the line that begins with prefix, plus the lines glamour
// wrapped it onto — followed by matching the words of text one by one. ok is
// false when no line begins with prefix.
func markBlock(lines []string, from, to int, prefix, text string) (first, last int, ok bool) {
	words := strings.Fields(text)
	for l := from; l < to && l < len(lines); l++ {
		t := strings.TrimLeft(strings.TrimRight(stripANSI(lines[l]), " "), " ")
		if !strings.HasPrefix(t, prefix) {
			continue
		}
		got := strings.Fields(strings.TrimPrefix(t, prefix))
		last = l
		for last+1 < to && last+1 < len(lines) && len(got) < len(words) {
			more := strings.Fields(stripANSI(lines[last+1]))
			if len(more) == 0 || len(got)+len(more) > len(words) || !equalWords(words[len(got):len(got)+len(more)], more) {
				break
			}
			got = append(got, more...)
			last++
		}
		return l, last, true
	}
	return 0, 0, false
}

func equalWords(a, b []string) bool {
	for i := range b {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// reflow lays "prefix text" out under an item: indented two, wrapped to width,
// continuation lines hanging under the text, every line in style.
func reflow(prefix, text string, width int, style lipgloss.Style) []string {
	const indent = "  "
	hang := strings.Repeat(" ", len(indent)+visibleWidth(prefix))
	cur, curW, fresh := indent+prefix, len(indent)+visibleWidth(prefix), true
	var rows []string
	for _, w := range strings.Fields(text) {
		ww := visibleWidth(w)
		if !fresh && curW+1+ww > width {
			rows = append(rows, style.Render(cur))
			cur, curW, fresh = hang, len(hang), true
		}
		if !fresh {
			cur += " "
			curW++
		}
		cur += w
		curW += ww
		fresh = false
	}
	return append(rows, style.Render(cur))
}

// rewriteMarks redraws each mapped item's question and reply as an indented,
// wrapped block in one color — glamour wraps them at column 0 and leaves the
// wrapped lines uncolored.
func rewriteMarks(lines []string, starts [][]int, board Board, width int) []string {
	type swap struct {
		first, last int
		rows        []string
	}
	var swaps []swap
	for si, ss := range starts {
		for ii, start := range ss {
			it := board.Sections[si].Items[ii]
			end := itemEnd(lines, ss, ii)
			for _, mk := range []struct {
				prefix, text string
				style        lipgloss.Style
			}{
				{questionPrefix, plainMark(askText(it)), questionStyle},
				{replyPrefix, plainMark(answerOf(it)), replyLineStyle},
			} {
				if mk.text == "" {
					continue
				}
				if f, l, ok := markBlock(lines, start+1, end, mk.prefix, mk.text); ok {
					swaps = append(swaps, swap{f, l, reflow(mk.prefix, mk.text, width, mk.style)})
				}
			}
		}
	}
	sort.Slice(swaps, func(i, j int) bool { return swaps[i].first < swaps[j].first })
	var out []string
	next := 0
	for i := 0; i < len(lines); i++ {
		if next < len(swaps) && i == swaps[next].first {
			out = append(out, swaps[next].rows...)
			i = swaps[next].last
			next++
			continue
		}
		out = append(out, lines[i])
	}
	return out
}

// itemEnd is the index one past an item's last rendered line: the next
// item's start, or the first blank line, backed up over trailing blanks.
func itemEnd(lines []string, starts []int, ii int) int {
	start := starts[ii]
	end := start + 1
	if ii+1 < len(starts) {
		end = starts[ii+1]
	} else {
		for end < len(lines) && strings.TrimSpace(stripANSI(lines[end])) != "" {
			end++
		}
	}
	for end-1 > start && strings.TrimSpace(stripANSI(lines[end-1])) == "" {
		end--
	}
	return end
}

// spaceItems puts one blank line after every item whose next line is not
// already blank, so a long list does not read as a wall.
func spaceItems(lines []string, starts [][]int) []string {
	insert := map[int]bool{}
	for _, ss := range starts {
		for ii := range ss {
			end := itemEnd(lines, ss, ii)
			if end < len(lines) && strings.TrimSpace(stripANSI(lines[end])) != "" {
				insert[end] = true
			}
		}
	}
	var out []string
	for i, ln := range lines {
		if insert[i] {
			out = append(out, "")
		}
		out = append(out, ln)
	}
	return out
}

// hoverKind is what the pointer is over.
type hoverKind int

const (
	hoverNone hoverKind = iota
	hoverBullet
	hoverQuestion
	hoverLink
)

// hoverTarget is the clickable thing under the pointer: the rendered lines
// it covers (line through last) and what it is.
type hoverTarget struct {
	line, last int
	kind       hoverKind
	link       linkSpan // for hoverLink
}

// bulletCells is how many columns from the left edge tick an item — the
// bullet, its space, and a little slack, so the target is easy to hit.
const bulletCells = 4

// isQuestionLine reports a rendered line that starts a question.
func isQuestionLine(rendered string) bool {
	return strings.HasPrefix(strings.TrimLeft(strings.TrimRight(stripANSI(rendered), " "), " "), questionPrefix)
}

// paintHover repaints what the pointer is over, on lines the caller has
// already copied: the leading cells of a bullet turn solid, and every line of
// a question goes bold and underlined.
func paintHover(lines []string, h hoverTarget) {
	switch h.kind {
	case hoverLink:
		lines[h.line] = paintLink(lines[h.line], h.link)
	case hoverBullet:
		l := lines[h.line]
		lines[h.line] = bulletHotStyle.Render(stripANSI(ansi.Cut(l, 0, bulletCells))) + ansi.Cut(l, bulletCells, visibleWidth(l))
	case hoverQuestion:
		for i := h.line; i <= h.last && i < len(lines); i++ {
			lines[i] = questionHotStyle.Render(strings.TrimRight(stripANSI(lines[i]), " "))
		}
	}
}
