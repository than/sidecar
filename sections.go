// sections.go
package main

import (
	"strings"
	"unicode"
)

// leadingEmoji reports whether label's first whitespace-separated field is a
// leading emoji/symbol — "starts with an emoji" ≈ the first field's first
// rune is a symbol. Shared by sectionTag (semdiff.go, for the short diff-line
// tag) and sectionFromLabel (below, for splitting a heading into
// Section.Emoji/Name); ok is false when label is a single field or its first
// field isn't a symbol.
func leadingEmoji(label string) (emoji string, ok bool) {
	fields := strings.Fields(label)
	if len(fields) > 1 {
		r := []rune(fields[0])[0]
		if unicode.IsSymbol(r) || r > 0x2600 {
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
		b.WriteString(s.Header() + "\n\n- nothing yet\n\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}
