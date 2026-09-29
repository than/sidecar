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
	chipRun = regexp.MustCompile(`\[ [^\]]+ \]`)
	countIn = regexp.MustCompile(` \(\d+\)$`)
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

func codeSpans(chips []string) string {
	spans := make([]string, len(chips))
	for i, c := range chips {
		spans[i] = "`" + c + "`"
	}
	return strings.Join(spans, " ")
}

// buttonize adds each open item's action row and redraws its Ask: line. It
// may add lines, so run it on the text that will actually be rendered, after
// applyCollapse; board must be parseBoard of that same text.
func buttonize(raw string, board Board) string {
	lines := strings.Split(raw, "\n")
	replace := map[int]string{}
	after := map[int]string{}
	for _, s := range board.Sections {
		for _, it := range s.Items {
			if !actionable(s.Label, it) {
				continue
			}
			for i := it.StartLine + 1; i <= it.EndLine && i < len(lines); i++ {
				if !strings.HasPrefix(strings.TrimSpace(lines[i]), "Ask:") {
					continue
				}
				indent := lines[i][:len(lines[i])-len(strings.TrimLeft(lines[i], " \t"))]
				if opts := optionChips(it); len(opts) > 0 {
					replace[i] = indent + codeSpans(opts)
				} else if q := askText(it); q != "" {
					replace[i] = indent + "💬 " + q
				}
				break
			}
			after[it.EndLine] = "  " + codeSpans(rowChips(it))
		}
	}
	var out []string
	for i, ln := range lines {
		if r, ok := replace[i]; ok {
			ln = r
		}
		out = append(out, ln)
		if row, ok := after[i]; ok {
			out = append(out, row)
		}
	}
	return strings.Join(out, "\n")
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

// styleButtons repaints every line made only of buttons with solid colors —
// blue, and green for Done and a recorded choice — so they read as controls
// rather than inline code. Visible text is unchanged, so chipAt still finds
// each button's columns.
func styleButtons(lines []string) []string {
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = ln
		t := stripANSI(ln)
		body := strings.TrimSpace(t)
		if !strings.HasPrefix(body, "[ ") || strings.TrimSpace(chipRun.ReplaceAllString(body, "")) != "" {
			continue
		}
		indent := t[:len(t)-len(strings.TrimLeft(t, " "))]
		var parts []string
		for _, c := range chipRun.FindAllString(body, -1) {
			if c == doneChip || strings.HasPrefix(c, "[ ✓ ") {
				parts = append(parts, buttonGoStyle.Render(c))
			} else {
				parts = append(parts, buttonStyle.Render(c))
			}
		}
		out[i] = indent + strings.Join(parts, "  ")
	}
	return out
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
