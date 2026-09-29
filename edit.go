// edit.go — the viewer's only writes to the board: tick a checklist item,
// answer an Ask: prompt, or move an item to ✅ Done. Every edit is a pure
// text transform on one item, located by section label and exact item text
// in the file as it is on disk right now, so an unrelated edit by the agent
// between render and keypress is preserved and a conflicting one is refused.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var errBoardChanged = errors.New("board changed under the cursor — nothing written")

// checkboxPrefixes are the task-list markers on an item's first line.
const (
	unticked = "- [ ] "
	ticked   = "- [x] "
)

// isCheckbox reports whether an item's first line is a task-list item.
func isCheckbox(it BoardItem) bool {
	first := firstLine(it.Raw)
	return strings.HasPrefix(first, unticked) || strings.HasPrefix(strings.ToLower(first), ticked)
}

func firstLine(raw string) string {
	first, _, _ := strings.Cut(raw, "\n")
	return first
}

// toggleCheckbox flips "- [ ]" and "- [x]" on the item's first line.
func toggleCheckbox(lines []string) ([]string, error) {
	out := append([]string(nil), lines...)
	switch {
	case strings.HasPrefix(out[0], unticked):
		out[0] = ticked + strings.TrimPrefix(out[0], unticked)
	case strings.HasPrefix(strings.ToLower(out[0]), ticked):
		out[0] = unticked + out[0][len(ticked):]
	default:
		return nil, errors.New("not a checklist item")
	}
	return out, nil
}

// askOptions returns the options of an item's "Ask:" line — "Ask: yes | no"
// yields ["yes", "no"] — or nil when the item asks nothing.
func askOptions(it BoardItem) []string {
	_, rest, _ := strings.Cut(it.Raw, "\n")
	for _, ln := range strings.Split(rest, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(ln), "Ask:"); ok {
			var opts []string
			for _, o := range strings.Split(v, "|") {
				if o = strings.TrimSpace(o); o != "" {
					opts = append(opts, o)
				}
			}
			return opts
		}
	}
	return nil
}

// hasAsk reports whether the item carries an "Ask:" line at all — options or
// not. An Ask: with no options is an open question, answered in free text.
func hasAsk(it BoardItem) bool {
	_, rest, _ := strings.Cut(it.Raw, "\n")
	for _, ln := range strings.Split(rest, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "Ask:") {
			return true
		}
	}
	return false
}

// isUnanswered reports a question still waiting on the human.
func isUnanswered(it BoardItem) bool { return hasAsk(it) && answerOf(it) == "" }

// cleanAnswer reduces typed text to one plain line: control characters and
// newlines become spaces, runs of space collapse, and the ends are trimmed.
func cleanAnswer(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// answerOf returns the item's current "Answer:" value, "" when unanswered.
func answerOf(it BoardItem) string {
	_, rest, _ := strings.Cut(it.Raw, "\n")
	for _, ln := range strings.Split(rest, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(ln), "Answer:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// setAnswer writes "Answer: <option>" directly under the Ask: line,
// replacing an earlier answer so the item never carries two.
func setAnswer(lines []string, option string) ([]string, error) {
	askAt := -1
	for i, ln := range lines[1:] {
		if strings.HasPrefix(strings.TrimSpace(ln), "Ask:") {
			askAt = i + 1
			break
		}
	}
	if askAt < 0 {
		return nil, errors.New("no Ask: line on this item")
	}
	indent := lines[askAt][:len(lines[askAt])-len(strings.TrimLeft(lines[askAt], " \t"))]
	var out []string
	for i, ln := range lines {
		if i > 0 && strings.HasPrefix(strings.TrimSpace(ln), "Answer:") {
			continue
		}
		out = append(out, ln)
		if i == askAt {
			out = append(out, indent+"Answer: "+option)
		}
	}
	return out, nil
}

// moveItem returns raw with the item at (si, ii) moved to the end of the
// section labeled target, replacing that section's "nothing yet" placeholder.
func moveItem(raw string, b Board, si, ii int, target string) (string, error) {
	ti := -1
	for i, s := range b.Sections {
		if s.Label == target {
			ti = i
		}
	}
	if ti < 0 {
		return "", fmt.Errorf("no %q section", target)
	}
	if ti == si {
		return "", errors.New("already there")
	}
	lines := strings.Split(raw, "\n")
	it := b.Sections[si].Items[ii]
	block := append([]string(nil), lines[it.StartLine:it.EndLine+1]...)

	ts := b.Sections[ti]
	skip := make([]bool, len(lines))
	for i := it.StartLine; i <= it.EndLine; i++ {
		skip[i] = true
	}
	at := ts.HeaderLine + 1 // insert before this original index
	blankFirst := false
	real := 0
	for _, x := range ts.Items {
		if x.Key == emptySectionPlaceholder {
			for i := x.StartLine; i <= x.EndLine; i++ {
				skip[i] = true
			}
			at = x.StartLine
			continue
		}
		real++
		at = x.EndLine + 1
	}
	if real == 0 && len(ts.Items) == 0 {
		if at < len(lines) && strings.TrimSpace(lines[at]) == "" {
			at++
		} else {
			blankFirst = true
		}
	}

	var out []string
	for i := 0; i <= len(lines); i++ {
		if i == at {
			if blankFirst {
				out = append(out, "")
			}
			out = append(out, block...)
			if blankFirst {
				out = append(out, "")
			}
		}
		if i < len(lines) && !skip[i] {
			out = append(out, lines[i])
		}
	}
	return strings.Join(out, "\n"), nil
}

// editItem re-reads the board, finds the item by section label and exact
// text, and lets fn produce the new file content. errBoardChanged means the
// item is gone or was edited since the viewer rendered it.
func editItem(path, label, oldRaw string, fn func(raw string, b Board, si, ii int) (string, error)) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	raw := string(data)
	b, ok := parseBoard(raw)
	if !ok {
		return errBoardChanged
	}
	for si, s := range b.Sections {
		if s.Label != label {
			continue
		}
		for ii, it := range s.Items {
			if it.Raw == oldRaw {
				next, err := fn(raw, b, si, ii)
				if err != nil {
					return err
				}
				return writeAtomic(path, next)
			}
		}
	}
	return errBoardChanged
}

// replaceLines is the common shape of tick and answer: swap one item's
// lines for the transform of them.
func replaceLines(t func([]string) ([]string, error)) func(string, Board, int, int) (string, error) {
	return func(raw string, b Board, si, ii int) (string, error) {
		it := b.Sections[si].Items[ii]
		lines := strings.Split(raw, "\n")
		repl, err := t(lines[it.StartLine : it.EndLine+1])
		if err != nil {
			return "", err
		}
		out := append(append(append([]string(nil), lines[:it.StartLine]...), repl...), lines[it.EndLine+1:]...)
		return strings.Join(out, "\n"), nil
	}
}

// writeAtomic swaps the file in via rename so a watcher or the agent never
// reads a half-written board, keeping the original file mode.
func writeAtomic(path, content string) error {
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sidecar-edit-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
