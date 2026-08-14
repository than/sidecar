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

// Header renders the markdown heading line, e.g. "## 🧠 Needs you", or
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

// defaultSections is the built-in set, used whenever the user doesn't pick a
// custom set (piped stdin, cancel, or an emptied list). 🧠 names its reader so
// there's no ambiguity about whose action it holds, and 🤖 gives queued work a
// home — without it, unstarted agent work lands in 🧠 (nothing for the human to
// do) or 🚧 (not actually underway).
func defaultSections() []Section {
	return []Section{
		{"🧠", "Needs you", "the human is the blocker"},
		{"🤖", "Agent queue", "queued for an agent, not started"},
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

// roleSection finds the configured section that plays the same role as want —
// one of the built-in defaults. Position carries no meaning: the picker lets
// the human reorder, rename, re-emoji and delete every section, and an init
// re-run adopts whatever headings an existing board already has. Indexing
// sections[0] as "the human's" therefore bakes a false rule into the board of
// anyone who moved or dropped it.
//
// Emoji is the most durable handle — renaming a section is common, changing
// its emoji is not — with name and hint as fallbacks. ok is false when the
// role is gone entirely; callers then say nothing rather than pointing the
// rule at a section that doesn't play the part.
func roleSection(sections []Section, want Section) (Section, bool) {
	for _, matches := range []func(Section) bool{
		func(s Section) bool { return want.Emoji != "" && s.Emoji == want.Emoji },
		func(s Section) bool { return s.Name == want.Name },
		func(s Section) bool { return want.Hint != "" && s.Hint == want.Hint },
	} {
		for _, s := range sections {
			if matches(s) {
				return s, true
			}
		}
	}
	return Section{}, false
}

// humanSection, agentSection and progressSection resolve the three roles the
// entry-style rules and examples refer to by name.
func humanSection(sections []Section) (Section, bool) {
	return roleSection(sections, defaultSections()[0])
}

func agentSection(sections []Section) (Section, bool) {
	return roleSection(sections, defaultSections()[1])
}

func progressSection(sections []Section) (Section, bool) {
	return roleSection(sections, defaultSections()[2])
}

// entryStyleRules is the shared instruction set that keeps entries
// status-shaped: the shape of one entry, then which section it belongs in.
// Both the starter template comment (renderTemplate) and the CLAUDE.md note
// (claudeNote) render it, so the rules can't drift apart — whoever writes an
// entry has the board file open, and CLAUDE.md may not be in context at all.
// bullet is the caller's list marker. The placement rule names sections by
// role rather than by position, and says nothing when a role is absent, so it
// never points at a heading the board doesn't have or mislabels one it does.
func entryStyleRules(sections []Section, bullet string) string {
	var b strings.Builder
	b.WriteString("Write entries in Apple Developer documentation voice: declarative, front-loaded verb, present tense, one fact per sentence. State where things stand, not how they got there — no dates, no \"asked\", no \"then we decided\".\n\n")
	b.WriteString("One entry is at most:\n")
	for _, r := range []string{
		"a status tag and title on the first line",
		"two sentences of detail — more belongs in the PR or issue you link",
		"bare URLs, each on its own line",
		"one `Next:` line naming the single next action",
		"entry text on one line — never hard-wrap; the viewer wraps to the pane and source newlines become visible breaks",
	} {
		b.WriteString(bullet + r + "\n")
	}
	if human, ok := humanSection(sections); ok {
		b.WriteString("\n`" + human.Header() + "` only holds items where the human is the blocker, and every item there ends with a `Next:` line naming what they do. A finding, a question, or a status names no action")
		if agent, ok := agentSection(sections); ok {
			b.WriteString(" — it belongs in `" + agent.Header() + "` or a later section")
		} else {
			b.WriteString(" — it belongs in a later section")
		}
		b.WriteString(" until it needs a decision, and then the `Next:` line asks for that decision.\n")
	}
	return b.String()
}

// entryStyleExample is two worked wrong→right pairs, keyed to the failures
// that actually occur on boards in the wild: a narrative roll-up filed under
// the human-action section, and a well-shaped entry that still names no action.
// Abstract rules let both through — 82% of first-section entries surveyed
// carried no `Next:` line — so the templates ship examples too.
//
// Both pairs move an item out of the human section, so they need that section
// and somewhere to move to — resolved by role, never by position. Without a
// human section there's no wrong placement to demonstrate and nothing is
// emitted; each pair is then dropped independently if its own destination
// role is missing.
func entryStyleExample(sections []Section) string {
	human, ok := humanSection(sections)
	if !ok {
		return ""
	}
	agentSec, hasAgent := agentSection(sections)
	progSec, hasProg := progressSection(sections)
	if !hasProg {
		// The first pair's "right" side is work genuinely under way, so the
		// agent queue is a fair stand-in when there's no in-progress section.
		progSec, hasProg = agentSec, hasAgent
	}
	if !hasProg {
		return ""
	}
	first, prog := human.Header(), progSec.Header()
	out := "Story in the wrong section (wrong):\n\n" +
		first + "\n" +
		"- Per-app PRs are owned by their sessions — #259 (Checkout), #239 → #243 (Billing), #256 (Admin, still parked on you creating the \"Admin (Development)\" API key), and the Reports app's store submission. Ask each session for status rather than this queue. Cross-cutting note that outlives them: #239 and #259 both add a vitest suite to the same test:all line, so whichever merges second needs a rebase.\n\n" +
		"Split by who acts (right):\n\n" +
		first + "\n" +
		"- #256 (Admin) is blocked: it needs an \"Admin (Development)\" API key that only you can create.\n" +
		"  https://github.com/o/r/pull/256\n" +
		"  Next: Create the key in the provider dashboard.\n\n" +
		prog + "\n" +
		// #256 is deliberately absent here: it's already listed above as the
		// human's blocked item, and an example that files one item under two
		// headings undercuts "move each item to the section matching its state".
		"- Per-app PRs run in their own sessions: #259 and #243, plus the Reports app's store submission. Ask each session for status.\n" +
		"- #239 and #259 both add a vitest suite to the same `test:all` line — whichever merges second rebases.\n"
	// The queue pair's whole lesson is where unstarted work goes. With no
	// agent-queue section there's nowhere correct to point it — retargeting it
	// at the in-progress section would teach filing unstarted work as in
	// progress, the exact failure it exists to prevent. Drop it; the first pair
	// still carries the placement lesson.
	if !hasAgent {
		return out
	}
	return out + "\n" +
		"No action named (wrong):\n\n" +
		first + "\n" +
		"- #440 — the Ohio return may be filing only the local increment where the other states layer state and local separately. Largest open question.\n\n" +
		"Queue the work instead (right):\n\n" +
		agentSec.Header() + "\n" +
		"- #440 — check whether the Ohio return files only the local increment where the other states layer state and local separately.\n" +
		"  https://github.com/o/r/issues/440\n"
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
	b.WriteString("\n" + entryStyleRules(sections, "· "))
	if ex := entryStyleExample(sections); ex != "" {
		b.WriteString("\n" + ex)
	}
	b.WriteString("-->\n\n")
	for _, s := range sections {
		b.WriteString(s.Header() + "\n\n- " + emptySectionPlaceholder + "\n\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}
