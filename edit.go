// edit.go — the viewer's only writes to the board, each a pure text transform
// on one item: tick it (• ↔ ✓) or set its reply. The viewer never moves an
// item between sections; that is the agent's job. Every edit locates the item
// by section label and exact text in the file as it is on disk right now, so
// an unrelated edit by the agent between render and click is preserved and a
// conflicting one is refused.
package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var errBoardChanged = errors.New("board changed under the cursor — nothing written")

const (
	unticked = "- [ ] "
	ticked   = "- [x] "
)

func firstLine(raw string) string {
	first, _, _ := strings.Cut(raw, "\n")
	return first
}

// isTicked reports an item whose first line is "- [x] …" — drawn ✓.
func isTicked(it BoardItem) bool {
	return strings.HasPrefix(strings.ToLower(firstLine(it.Raw)), ticked)
}

// toggleTick turns a plain or "- [ ]" item into "- [x]", and a ticked one back
// into a plain bullet. Ticking is the human saying "I did this"; unticking
// takes it back.
func toggleTick(lines []string) ([]string, error) {
	out := append([]string(nil), lines...)
	first := out[0]
	switch {
	case strings.HasPrefix(strings.ToLower(first), ticked):
		out[0] = "- " + first[len(ticked):]
	case strings.HasPrefix(first, unticked):
		out[0] = ticked + strings.TrimPrefix(first, unticked)
	case strings.HasPrefix(first, "- "):
		out[0] = ticked + strings.TrimPrefix(first, "- ")
	default:
		return nil, errors.New("not a list item")
	}
	return out, nil
}

// hasAsk reports whether the item carries an "Ask:" line — the agent's
// question, which the human answers in their own words.
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

// setAnswer writes "Answer: <text>" directly under the Ask: line — or last,
// when the item asked nothing — replacing an earlier answer so the item never
// carries two.
func setAnswer(lines []string, text string) ([]string, error) {
	askAt := -1
	for i, ln := range lines[1:] {
		if strings.HasPrefix(strings.TrimSpace(ln), "Ask:") {
			askAt = i + 1
			break
		}
	}
	var out []string
	for i, ln := range lines {
		if i > 0 && strings.HasPrefix(strings.TrimSpace(ln), "Answer:") {
			continue
		}
		out = append(out, ln)
		if i == askAt {
			indent := ln[:len(ln)-len(strings.TrimLeft(ln, " \t"))]
			out = append(out, indent+"Answer: "+text)
		}
	}
	if askAt < 0 {
		out = append(out, "  Answer: "+text)
	}
	return out, nil
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

// replaceLines swaps one item's lines for the transform of them.
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
