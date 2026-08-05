// board.go — parse a board file into sections and items for the semantic diff.
package main

import "strings"

const sidecarDirName = ".sidecar"

// BoardItem is one top-level bullet plus its continuation lines. Key is the
// normalized first line — the identity items are matched by across versions.
type BoardItem struct {
	Key string
	Raw string
}

// BoardSection is one "## " heading and the items under it. Label is the
// heading without the "## " prefix.
type BoardSection struct {
	Label string
	Items []BoardItem
}

type Board struct {
	Sections []BoardSection
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
	for _, ln := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(ln)
		if inComment {
			if strings.Contains(ln, "-->") {
				inComment = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "<!--") && !strings.Contains(trimmed, "-->") {
			inComment = true
			continue
		}
		switch {
		case strings.HasPrefix(ln, "## "):
			b.Sections = append(b.Sections, BoardSection{Label: strings.TrimSpace(strings.TrimPrefix(ln, "## "))})
			cur = &b.Sections[len(b.Sections)-1]
			item = nil
		case cur == nil:
			// Preamble before the first heading — title, comments. Skip.
		case strings.HasPrefix(strings.TrimLeft(ln, " \t"), "- ") && !strings.HasPrefix(ln, " ") && !strings.HasPrefix(ln, "\t"):
			cur.Items = append(cur.Items, BoardItem{Key: normalizeItem(ln), Raw: ln})
			item = &cur.Items[len(cur.Items)-1]
		case trimmed == "":
			item = nil
		case item != nil:
			item.Raw += "\n" + ln
		}
	}
	return b, len(b.Sections) > 0
}

// normalizeItem strips the bullet and collapses whitespace on the first line.
func normalizeItem(line string) string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "- ")
	return strings.Join(strings.Fields(s), " ")
}
