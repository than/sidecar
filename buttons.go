// buttons.go — draw an item's Ask: line as buttons. The board file keeps
// the plain "Ask: yes | no" line; the viewer swaps it, at display time, for
// shaded chips — [ yes ]  [ no ]  [ ✎ ] — with the chosen answer ticked.
// In mouse mode a click on a chip answers.
package main

import (
	"strings"
	"unicode"
)

// freeChip is the button that opens the free-text answer line.
const freeChip = "[ ✎ ]"

// chipText is one option as drawn: "[ ✅ Done ]", or "[ ✓ ✅ Done ]" once
// it is the recorded answer.
func chipText(opt string, chosen bool) string {
	opt = strings.ReplaceAll(opt, "`", "")
	if chosen {
		return "[ ✓ " + opt + " ]"
	}
	return "[ " + opt + " ]"
}

// chips returns an item's buttons in order, with the free-text button last.
func chips(it BoardItem) []string {
	ans := answerOf(it)
	var out []string
	for _, o := range askOptions(it) {
		out = append(out, chipText(o, ans != "" && strings.EqualFold(ans, o)))
	}
	return append(out, freeChip)
}

// buttonize rewrites every Ask: line as its buttons, one code span each.
// The line count never changes, so board line indices stay valid for
// applyCollapse.
func buttonize(raw string, board Board) string {
	lines := strings.Split(raw, "\n")
	for _, s := range board.Sections {
		for _, it := range s.Items {
			if !hasAsk(it) {
				continue
			}
			for i := it.StartLine + 1; i <= it.EndLine && i < len(lines); i++ {
				trimmed := strings.TrimSpace(lines[i])
				if !strings.HasPrefix(trimmed, "Ask:") {
					continue
				}
				indent := lines[i][:len(lines[i])-len(strings.TrimLeft(lines[i], " \t"))]
				var spans []string
				for _, c := range chips(it) {
					spans = append(spans, "`"+c+"`")
				}
				lines[i] = indent + strings.Join(spans, " ")
				break
			}
		}
	}
	return strings.Join(lines, "\n")
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

// chipAt finds which of the item's buttons covers display column x on a
// rendered line: option is the answer to write, free is the ✎ button. ok is
// false when x is not on a button.
func chipAt(it BoardItem, renderedLine string, x int) (option string, free, ok bool) {
	text := stripANSI(renderedLine)
	opts := askOptions(it)
	for i, c := range chips(it) {
		idx := strings.Index(text, c)
		if idx < 0 {
			continue
		}
		col := visibleWidth(text[:idx])
		if x < col || x >= col+visibleWidth(c) {
			continue
		}
		if i < len(opts) {
			return opts[i], false, true
		}
		return "", true, true
	}
	return "", false, false
}
