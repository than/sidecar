// board.go — parse a board file into sections and items for the semantic diff.
package main

import (
	"regexp"
	"strings"
)

const sidecarDirName = ".sidecar"

// BoardItem is one top-level bullet plus its continuation lines. Key is the
// normalized first line — the identity items are matched by across versions.
// StartLine/EndLine are 0-indexed, inclusive line indices into the raw text
// split by "\n" — the exact lines applyCollapse (collapse.go) removes when
// this item's section is collapsed.
type BoardItem struct {
	Key       string
	Raw       string
	StartLine int
	EndLine   int
}

// BoardSection is one "## " heading and the items under it. Label is the
// heading without the "## " prefix. HeaderLine is the 0-indexed line index
// of the "## " line itself.
type BoardSection struct {
	Label      string
	Items      []BoardItem
	HeaderLine int
}

type Board struct {
	Sections []BoardSection
}

// sourceBullet matches a top-level list marker at the start of a source
// line: "-", "*", "+", or an ordered "1." / "1)". A board item is whichever
// of these the human or agent happened to write, so recognising only "- "
// left ordered and starred lists invisible — counted as zero in the section
// heading, skipped by the ▸ change pointer, and absent from every diff.
var sourceBullet = regexp.MustCompile(`^(?:[-*+]|\d{1,9}[.)])\s`)

// bulletBody strips a leading list marker and returns the rest. ok is false
// when the line doesn't open an item.
func bulletBody(line string) (string, bool) {
	m := sourceBullet.FindString(line)
	if m == "" {
		return "", false
	}
	return strings.TrimSpace(line[len(m):]), true
}

// parseBoard splits raw markdown into ## sections and their top-level
// bullets. Continuation lines (any non-blank, non-bullet, non-heading line
// after a bullet) belong to the preceding item; a blank line ends the item.
// ok is false when the file has no ## headings — callers then fall back to a
// plain textual diff.
func parseBoard(raw string) (Board, bool) {
	var b Board
	var cur *BoardSection
	var item *BoardItem
	inComment := false
	inFence := false
	rawLines := strings.Split(raw, "\n")
	for lineNo, ln := range rawLines {
		trimmed := strings.TrimSpace(ln)
		if inComment {
			if strings.Contains(ln, "-->") {
				inComment = false
			}
			continue
		}
		// A fenced code block's interior can contain lines that look like a
		// heading or a bullet ("## fake", "- fake"), or an unclosed "<!--";
		// none of that must be parsed as markup or comment syntax. This
		// check must come before the comment-open check below — an unclosed
		// "<!--" inside a fence is just literal fence content, not the start
		// of an HTML comment that would otherwise swallow every line after
		// it (including the closing fence) waiting for a "-->" that may
		// never appear. Toggle on the fence delimiters themselves and skip
		// section/item detection entirely while inside one — but a fence
		// line still belongs to whatever item is currently open, same as any
		// other continuation line.
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			if item != nil {
				item.Raw += "\n" + ln
				item.EndLine = lineNo
			}
			continue
		}
		if inFence {
			if item != nil {
				item.Raw += "\n" + ln
				item.EndLine = lineNo
			}
			continue
		}
		if strings.HasPrefix(trimmed, "<!--") {
			// A complete single-line comment ("<!-- x -->") needs no state —
			// skip it outright, same as the multi-line open below, so editing
			// its text doesn't attach to the preceding item's Raw.
			if !strings.Contains(trimmed, "-->") {
				inComment = true
			}
			continue
		}
		switch {
		case strings.HasPrefix(ln, "## "):
			b.Sections = append(b.Sections, BoardSection{
				Label:      strings.TrimSpace(strings.TrimPrefix(ln, "## ")),
				HeaderLine: lineNo,
			})
			cur = &b.Sections[len(b.Sections)-1]
			item = nil
		case cur == nil:
			// Preamble before the first heading — title, comments. Skip.
		case sourceBullet.MatchString(ln):
			cur.Items = append(cur.Items, BoardItem{
				Key:       normalizeItem(ln),
				Raw:       ln,
				StartLine: lineNo,
				EndLine:   lineNo,
			})
			item = &cur.Items[len(cur.Items)-1]
		case trimmed == "":
			item = nil
		case item != nil:
			item.Raw += "\n" + ln
			item.EndLine = lineNo
		}
	}
	return b, len(b.Sections) > 0
}

// normalizeItem strips the bullet and collapses whitespace on the first line.
func normalizeItem(line string) string {
	s := strings.TrimSpace(line)
	// Drop whatever marker opened the item, not just "- ": keying on the
	// marker would make renumbering an ordered list read as every item
	// having changed.
	if body, ok := bulletBody(s); ok {
		s = body
	}
	return strings.Join(strings.Fields(s), " ")
}
