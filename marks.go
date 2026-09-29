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
	"strings"

	"github.com/charmbracelet/lipgloss"
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
					lines[i] = indent + questionPrefix + strings.TrimSpace(strings.TrimPrefix(t, "Ask:"))
				case strings.HasPrefix(t, "Answer:"):
					lines[i] = indent + replyPrefix + strings.TrimSpace(strings.TrimPrefix(t, "Answer:"))
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
	return spaceItems(colorMarks(lines, starts), starts), nil
}

// colorMarks paints each question in the accent color and each reply green.
// Only lines inside a mapped item are touched, so body text that happens to
// begin "? " elsewhere is left alone.
func colorMarks(lines []string, starts [][]int) []string {
	out := append([]string(nil), lines...)
	for _, ss := range starts {
		for ii, start := range ss {
			end := itemEnd(lines, ss, ii)
			for l := start + 1; l < end && l < len(out); l++ {
				t := strings.TrimRight(stripANSI(out[l]), " ")
				switch {
				case strings.HasPrefix(t, questionPrefix):
					out[l] = questionStyle.Render(t)
				case strings.HasPrefix(t, replyPrefix):
					out[l] = replyLineStyle.Render(t)
				}
			}
		}
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
)

// hoverTarget is the clickable thing under the pointer: which rendered line
// and what it is.
type hoverTarget struct {
	line int
	kind hoverKind
}

// isQuestionLine reports a rendered line that is an item's question.
func isQuestionLine(rendered string) bool {
	return strings.HasPrefix(strings.TrimRight(stripANSI(rendered), " "), questionPrefix)
}

// paintHover repaints the hovered bullet or question on a copy-safe line: the
// bullet glyph turns solid, the question goes bold and underlined.
func paintHover(line string, kind hoverKind) string {
	switch kind {
	case hoverBullet:
		for _, g := range []string{"•", "✓", "□"} {
			if strings.Contains(line, g) {
				return strings.Replace(line, g, bulletHotStyle.Render(g), 1)
			}
		}
	case hoverQuestion:
		return questionHotStyle.Render(strings.TrimRight(stripANSI(line), " "))
	}
	return line
}
