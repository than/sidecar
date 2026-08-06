// sections.go
package main

import (
	"strings"
	"unicode"
)

// isEmojiRune reports whether r is plausibly an emoji: a Unicode "other
// symbol" (So, e.g. ✅ 📦), or a rune in one of the explicit emoji blocks —
// pictographs (0x1F300–0x1FAFF), the misc-symbols/dingbats band including
// arrows and stars (0x2600–0x27BF), misc symbols and arrows-B
// (0x2B00–0x2BFF), or the variation selector that renders a preceding glyph
// as emoji (0xFE0F). A blanket "r > 0x2600" also matched CJK ideographs,
// kana, and Hangul (all well above 0x2600), so it's intentionally not used.
func isEmojiRune(r rune) bool {
	if unicode.In(r, unicode.So) {
		return true
	}
	switch {
	case r >= 0x1F300 && r <= 0x1FAFF,
		r >= 0x2600 && r <= 0x27BF,
		r >= 0x2B00 && r <= 0x2BFF,
		r == 0xFE0F:
		return true
	}
	return false
}

// leadingEmoji reports whether label's first whitespace-separated field is a
// leading emoji. Shared by sectionTag (semdiff.go, for the short diff-line
// tag) and sectionFromLabel (below, for splitting a heading into
// Section.Emoji/Name); ok is false when label is a single field or its first
// field's first rune isn't emoji (including any other non-ASCII text, like
// CJK, that isn't).
func leadingEmoji(label string) (emoji string, ok bool) {
	fields := strings.Fields(label)
	if len(fields) > 1 {
		r := []rune(fields[0])[0]
		if isEmojiRune(r) {
			return fields[0], true
		}
	}
	return "", false
}

// Section is one queue heading. Emoji is optional (a text-only section like
// "Todo" is allowed); Hint is an optional one-line meaning that flows into the
// starter template comment and the CLAUDE.md note.
type Section struct {
	Emoji string
	Name  string
	Hint  string
}

// Header renders the markdown heading line, e.g. "## 🧠 Needs action", or
// "## Todo" when there's no emoji.
func (s Section) Header() string {
	if s.Emoji == "" {
		return "## " + s.Name
	}
	return "## " + s.Emoji + " " + s.Name
}

// label is the heading without the leading "## ".
func (s Section) label() string {
	return strings.TrimPrefix(s.Header(), "## ")
}

// defaultSections is the built-in five, used whenever the user doesn't pick a
// custom set (piped stdin, cancel, or an emptied list).
func defaultSections() []Section {
	return []Section{
		{"🧠", "Needs action", "surfaced for the human to act on"},
		{"🚧", "In progress", "actively being worked"},
		{"🚘", "Parked", "deferred, not dropped"},
		{"✅", "Done", "merged, not yet released"},
		{"📦", "Shipped", "released (tag the version)"},
	}
}

// emptySectionPlaceholder is the bullet every scaffolded section starts
// with. semanticDiff (semdiff.go) treats it as identity-free — it recurs by
// design across sections and versions and must never be reported as an item
// that "moved".
const emptySectionPlaceholder = "nothing yet"

// renderTemplate builds the starter file body for the chosen sections. Bare
// URLs on their own line stay clickable; hintless sections are omitted from
// the comment's "= meaning" list.
func renderTemplate(sections []Section) string {
	var b strings.Builder
	b.WriteString("# Sidecar\n\n")
	b.WriteString("<!--\n")
	b.WriteString("Sidecar review queue — agent: keep this current as you work.\n")
	b.WriteString("· Keep the title and section headers as-is; only add, move, or remove items.\n")
	b.WriteString("· Move each item to the section matching its state.\n")
	for _, s := range sections {
		if s.Hint != "" {
			b.WriteString("· " + s.label() + " = " + s.Hint + "\n")
		}
	}
	b.WriteString("· Prune early sections as items move; let later ones accumulate as a log.\n")
	b.WriteString("· One line per item where you can; bare URLs on their own line stay clickable.\n")
	b.WriteString("-->\n\n")
	for _, s := range sections {
		b.WriteString(s.Header() + "\n\n- " + emptySectionPlaceholder + "\n\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}
