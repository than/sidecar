// sections.go
package main

import "strings"

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
		{"📦", "Shipped", "released"},
	}
}
