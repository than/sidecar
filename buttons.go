// buttons.go — the action row under every open item. The board file stays
// plain text; at display time the viewer draws, beneath each item outside
// ✅ Done and 📦 Shipped, shaded buttons:
//
//	[ ✅ Done ]  [ 💬 Reply ]
//
// Reply opens a free-text line (pre-filled with the current reply), Done
// moves the item to ✅ Done. An "Ask: a | b" line draws its choices as
// buttons of their own; a narrative "Ask: what did you change?" line shows as
// the question the reply answers. Clicking any button needs no item selected.
//
// The rows are drawn after rendering, not written into the markdown: see
// addButtonRows.
package main

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

const (
	doneChip      = "[ ✅ Done ]"
	replyChip     = "[ 💬 Reply ]"
	editReplyChip = "[ 💬 Edit reply ]"
)

var (
	buttonStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("#101010")).Background(lipgloss.Color("#7AA2F7"))
	buttonGoStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("#101010")).Background(lipgloss.Color("#9ECE6A"))
	buttonRestStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#8B93A5"))
	buttonChosenStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#9ECE6A"))
	countIn           = regexp.MustCompile(` \(\d+\)$`)
)

// finishedLabel reports a ✅ Done or 📦 Shipped heading, with or without the
// " (N)" item count the display rewrite appends.
func finishedLabel(label string) bool {
	label = countIn.ReplaceAllString(label, "")
	return strings.HasSuffix(label, "Done") || strings.HasSuffix(label, "Shipped")
}

// actionable is an item that gets an action row: real, and not finished.
func actionable(label string, it BoardItem) bool {
	return !finishedLabel(label) && it.Key != emptySectionPlaceholder
}

// chipText is one choice as drawn: "[ yes ]", or "[ ✓ yes ]" once recorded.
func chipText(opt string, chosen bool) string {
	opt = strings.ReplaceAll(opt, "`", "")
	if chosen {
		return "[ ✓ " + opt + " ]"
	}
	return "[ " + opt + " ]"
}

// optionChips are the buttons of an "Ask: a | b" line, the recorded one ticked.
func optionChips(it BoardItem) []string {
	ans := answerOf(it)
	var out []string
	for _, o := range askOptions(it) {
		out = append(out, chipText(o, ans != "" && strings.EqualFold(ans, o)))
	}
	return out
}

func replyChipFor(it BoardItem) string {
	if answerOf(it) != "" {
		return editReplyChip
	}
	return replyChip
}

// rowChips are the buttons under an item, in order.
func rowChips(it BoardItem) []string { return []string{doneChip, replyChipFor(it)} }

// buttonize redraws each open item's Ask: line: a narrative question shows as
// "💬 <question>", and a choice line ("Ask: a | b") as "💬 Choose:" — its
// choices become buttons in the row below the item. The line count never
// changes; the rows themselves are added after rendering (addButtonRows),
// because glamour fuses any paragraph added under a list item onto its last
// line and would wrap a button mid-label.
func buttonize(raw string, board Board) string {
	lines := strings.Split(raw, "\n")
	for _, s := range board.Sections {
		for _, it := range s.Items {
			if !actionable(s.Label, it) || !hasAsk(it) {
				continue
			}
			for i := it.StartLine + 1; i <= it.EndLine && i < len(lines); i++ {
				if !strings.HasPrefix(strings.TrimSpace(lines[i]), "Ask:") {
					continue
				}
				indent := lines[i][:len(lines[i])-len(strings.TrimLeft(lines[i], " \t"))]
				if q := askText(it); q != "" {
					lines[i] = indent + "💬 " + q
				} else {
					lines[i] = indent + "💬 Choose:"
				}
				break
			}
		}
	}
	return strings.Join(lines, "\n")
}

// renderBoardLines renders the display markdown and adds every open item's
// button row. board must be the parse of the file the display text came from,
// so items line up with the rendered bullets.
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
	return addButtonRows(lines, starts, board, width), nil
}

// addButtonRows inserts, below the last line of each mapped open item, its
// buttons — choices first, then Done and Reply — packed to the pane width. An
// item whose section could not be mapped to rendered lines gets none.
func addButtonRows(lines []string, starts [][]int, board Board, width int) []string {
	insert := map[int][]string{}
	for si, s := range board.Sections {
		if si >= len(starts) {
			break
		}
		for ii, it := range s.Items {
			if ii >= len(starts[si]) || !actionable(s.Label, it) {
				continue
			}
			end := starts[si][ii] + 1
			if ii+1 < len(starts[si]) {
				end = starts[si][ii+1]
			} else {
				for end < len(lines) && strings.TrimSpace(stripANSI(lines[end])) != "" {
					end++
				}
			}
			// Back up over blank lines so the row hugs the item's last text.
			for end-1 > starts[si][ii] && strings.TrimSpace(stripANSI(lines[end-1])) == "" {
				end--
			}
			insert[end] = packButtons(append(optionChips(it), rowChips(it)...), width)
		}
	}
	// One blank line between items keeps a long list from reading as a wall:
	// after each item (and its buttons) whose next line is not already blank.
	for si, s := range board.Sections {
		if si >= len(starts) {
			break
		}
		for ii := range s.Items {
			if ii >= len(starts[si]) {
				continue
			}
			end := starts[si][ii] + 1
			if ii+1 < len(starts[si]) {
				end = starts[si][ii+1]
			} else {
				for end < len(lines) && strings.TrimSpace(stripANSI(lines[end])) != "" {
					end++
				}
			}
			for end-1 > starts[si][ii] && strings.TrimSpace(stripANSI(lines[end-1])) == "" {
				end--
			}
			if end < len(lines) && strings.TrimSpace(stripANSI(lines[end])) != "" {
				insert[end] = append(insert[end], "")
			}
		}
	}
	var out []string
	for i, ln := range lines {
		out = append(out, insert[i]...)
		out = append(out, ln)
	}
	return append(out, insert[len(lines)]...)
}

// packButtons lays styled buttons into lines no wider than width, two spaces
// apart and indented two; a button that does not fit starts the next line.
func packButtons(chips []string, width int) []string {
	const indent = "  "
	var rows []string
	cur, curW := indent, len(indent)
	for _, c := range chips {
		w := visibleWidth(c)
		if curW > len(indent) && curW+2+w > width {
			rows = append(rows, cur)
			cur, curW = indent, len(indent)
		}
		if curW > len(indent) {
			cur += "  "
			curW += 2
		}
		cur += styleChip(c)
		curW += w
	}
	return append(rows, cur)
}

// styleChip paints one button at rest: grey text on no background, so a
// screen of items is not a screen of color. A recorded choice keeps its green text.
func styleChip(c string) string {
	if strings.HasPrefix(c, "[ ✓ ") {
		return buttonChosenStyle.Render(c)
	}
	return buttonRestStyle.Render(c)
}

// hotChip paints a button under the pointer: solid green for Done, blue for
// the rest.
func hotChip(c string) string {
	if c == doneChip || strings.HasPrefix(c, "[ ✓ ") {
		return buttonGoStyle.Render(c)
	}
	return buttonStyle.Render(c)
}

// displayText is the board as rendered: collapsed sections dropped and counts
// added, then every open item's buttons.
func displayText(raw string, board Board, collapsed map[string]bool) string {
	collapsedRaw := applyCollapse(raw, board, collapsed)
	if b, ok := parseBoard(collapsedRaw); ok {
		return buttonize(collapsedRaw, b)
	}
	return collapsedRaw
}

// normalizeOption reduces an option to its letters and digits, lowercase,
// so "✅ Done" answers to d and "👍 Yes" to y.
func normalizeOption(o string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, o)
}

type chipKind int

const (
	chipOption chipKind = iota
	chipDone
	chipReply
)

// chipAt finds which of the item's buttons covers display column x on a
// rendered line; ok is false when x is not on a button. For chipOption,
// option is the choice to record.
func chipAt(it BoardItem, renderedLine string, x int) (kind chipKind, option string, ok bool) {
	text := stripANSI(renderedLine)
	hit := func(c string) bool {
		idx := strings.Index(text, c)
		if idx < 0 {
			return false
		}
		col := visibleWidth(text[:idx])
		return x >= col && x < col+visibleWidth(c)
	}
	for i, c := range optionChips(it) {
		if hit(c) {
			return chipOption, askOptions(it)[i], true
		}
	}
	switch {
	case hit(doneChip):
		return chipDone, "", true
	case hit(replyChipFor(it)):
		return chipReply, "", true
	}
	return 0, "", false
}

// chipTextAt is chipAt's twin for hover: the text of the button covering
// display column x, so it can be found again in the rendered line.
func chipTextAt(it BoardItem, renderedLine string, x int) (string, bool) {
	text := stripANSI(renderedLine)
	for _, c := range append(optionChips(it), rowChips(it)...) {
		idx := strings.Index(text, c)
		if idx < 0 {
			continue
		}
		col := visibleWidth(text[:idx])
		if x >= col && x < col+visibleWidth(c) {
			return c, true
		}
	}
	return "", false
}
