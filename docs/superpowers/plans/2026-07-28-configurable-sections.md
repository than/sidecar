# Configurable Sections Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let `sidecar init` write a user-chosen set of sections (emoji/name/hint/order) into the starter template, the CLAUDE.md note, and the reconcile hook, via an interactive bubbletea picker.

**Architecture:** Sections are a `Section{Emoji,Name,Hint}` value type in a new `sections.go`; the three init-time consumers are refactored to generate their text from an ordered `[]Section` instead of hardcoded strings. A new `picker.go` bubbletea program lets an interactive user toggle/reorder/edit/add sections; non-interactive stdin and any cancel/empty/error path fall back to `defaultSections()`. The viewer is untouched — it never parses sections.

**Tech Stack:** Go, bubbletea, bubbles (`textinput`, already a dependency via `viewport`), lipgloss. No new modules.

## Global Constraints

- Module path: `github.com/than/sidecar`; package `main` (flat, all `.go` at repo root).
- No new external dependencies — `github.com/charmbracelet/bubbles/textinput` ships in the already-imported `bubbles` module.
- Interactive UI runs ONLY when `stdinIsTerminal()` is true; piped/non-terminal stdin must use `defaultSections()` and never start a bubbletea program (tests run non-interactively).
- Never write a file with zero sections: cancel / all-deselected / all-empty / picker error → `defaultSections()`.
- The viewer and rendering code (`ui.go`, `style.go`, `watcher.go`) are NOT modified.
- Run `gofmt`-clean; the repo has no lint config beyond `go vet`.

---

### Task 1: Section model

**Files:**
- Create: `sections.go`
- Test: `sections_test.go`

**Interfaces:**
- Produces:
  - `type Section struct { Emoji, Name, Hint string }`
  - `func (s Section) Header() string` — `## 🧠 Needs action`, or `## Todo` when `Emoji == ""`
  - `func (s Section) label() string` — `Header()` without the leading `## `
  - `func defaultSections() []Section` — today's five, in order

- [ ] **Step 1: Write the failing test**

```go
// sections_test.go
package main

import "testing"

func TestSectionHeader(t *testing.T) {
	cases := []struct {
		in   Section
		want string
	}{
		{Section{"🧠", "Needs action", "x"}, "## 🧠 Needs action"},
		{Section{"", "Todo", ""}, "## Todo"},
	}
	for _, c := range cases {
		if got := c.in.Header(); got != c.want {
			t.Errorf("Header() = %q, want %q", got, c.want)
		}
	}
}

func TestSectionLabel(t *testing.T) {
	if got := (Section{"✅", "Done", ""}).label(); got != "✅ Done" {
		t.Errorf("label() = %q, want %q", got, "✅ Done")
	}
}

func TestDefaultSections(t *testing.T) {
	got := defaultSections()
	if len(got) != 5 {
		t.Fatalf("defaultSections len = %d, want 5", len(got))
	}
	wantEmoji := []string{"🧠", "🚧", "🚘", "✅", "📦"}
	for i, e := range wantEmoji {
		if got[i].Emoji != e {
			t.Errorf("section %d emoji = %q, want %q", i, got[i].Emoji, e)
		}
		if got[i].Name == "" || got[i].Hint == "" {
			t.Errorf("section %d missing name/hint: %+v", i, got[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run 'TestSection|TestDefaultSections' -v`
Expected: FAIL — `undefined: Section` / `undefined: defaultSections`

- [ ] **Step 3: Write minimal implementation**

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... -run 'TestSection|TestDefaultSections' -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add sections.go sections_test.go
git commit -m "feat: add Section model and defaults (#3)"
```

---

### Task 2: Template generation

**Files:**
- Create in `sections.go`: `renderTemplate`
- Modify: `init.go` — remove `starterTemplate` const, change `scaffold`, update both `scaffold(abs)` call sites to pass sections
- Test: `sections_test.go`

**Interfaces:**
- Consumes: `Section`, `defaultSections()` (Task 1)
- Produces:
  - `func renderTemplate(sections []Section) string`
  - `func scaffold(abs string, sections []Section) error` (signature changed from `scaffold(abs string) error`)

- [ ] **Step 1: Write the failing test**

```go
// sections_test.go (append)
import "strings" // add to existing import block if not present

func TestRenderTemplateDefault(t *testing.T) {
	out := renderTemplate(defaultSections())
	if !strings.HasPrefix(out, "# Sidecar\n") {
		t.Errorf("template missing title:\n%s", out)
	}
	for _, s := range defaultSections() {
		if !strings.Contains(out, s.Header()+"\n") {
			t.Errorf("template missing header %q:\n%s", s.Header(), out)
		}
		if !strings.Contains(out, "· "+s.label()+" = "+s.Hint) {
			t.Errorf("template comment missing hint for %q:\n%s", s.Name, out)
		}
	}
	if strings.Count(out, "- nothing yet") != 5 {
		t.Errorf("want 5 placeholder bullets, got %d", strings.Count(out, "- nothing yet"))
	}
	if !strings.HasSuffix(out, "\n") || strings.HasSuffix(out, "\n\n") {
		t.Errorf("template must end in exactly one newline:\n%q", out[len(out)-3:])
	}
}

func TestRenderTemplateCustomNoHint(t *testing.T) {
	out := renderTemplate([]Section{{"", "Todo", ""}})
	if !strings.Contains(out, "## Todo\n") {
		t.Errorf("missing text-only header:\n%s", out)
	}
	if strings.Contains(out, "Todo = ") {
		t.Errorf("hintless section should have no '= meaning' line:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestRenderTemplate -v`
Expected: FAIL — `undefined: renderTemplate`

- [ ] **Step 3: Write minimal implementation**

Add to `sections.go`:

```go
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
	b.WriteString("· One line per item where you can; bare URLs on their own line stay clickable.\n")
	b.WriteString("-->\n\n")
	for _, s := range sections {
		b.WriteString(s.Header() + "\n\n- nothing yet\n\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}
```

In `init.go`, delete the entire `starterTemplate` const block (the `const starterTemplate = \`...\`` through its closing backtick), and change `scaffold`:

```go
// scaffold writes the starter template for the chosen sections, leaving any
// existing file untouched.
func scaffold(abs string, sections []Section) error {
	if _, err := os.Stat(abs); err == nil {
		return nil
	}
	return os.WriteFile(abs, []byte(renderTemplate(sections)), 0o644)
}
```

Update the two call sites in `init.go` to pass sections for now (the picker is wired in Task 5):
- In `runInit`, change `scaffold(abs)` → `scaffold(abs, defaultSections())`
- In `offerCreate`, change `scaffold(abs)` → `scaffold(abs, defaultSections())`

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... -run 'TestRenderTemplate|TestInitScaffolds' -v`
Expected: PASS (existing `TestInitScaffolds` still finds all five emoji markers)

- [ ] **Step 5: Commit**

```bash
git add sections.go sections_test.go init.go
git commit -m "feat: generate starter template from sections (#3)"
```

---

### Task 3: CLAUDE.md note and reconcile hook take sections

**Files:**
- Modify: `init.go` — `claudeNote`, `writeClaudeNote`, `offerClaudeHook`, `reconcileMessage`, `reconcileHookEntry`, `writeReconcileHook`, and the `runInit` call to `offerClaudeHook`
- Modify: `init_test.go` — update existing `writeClaudeNote` / `writeReconcileHook` call sites; add custom-section assertions

**Interfaces:**
- Consumes: `Section`, `defaultSections()`, `label()` (Task 1)
- Produces (changed signatures):
  - `func claudeNote(rel string, sections []Section) string`
  - `func writeClaudeNote(root, rel string, sections []Section)`
  - `func offerClaudeHook(fileAbs string, sections []Section)`
  - `func reconcileMessage(rel string, sections []Section) string`
  - `func reconcileHookEntry(rel string, sections []Section) map[string]any`
  - `func writeReconcileHook(root, rel string, sections []Section)`

- [ ] **Step 1: Write the failing test**

Update the two existing call sites in `init_test.go`:
- `TestWriteClaudeNoteAppendsAndDedupes`: both `writeClaudeNote(root, "SIDECAR.md")` → `writeClaudeNote(root, "SIDECAR.md", defaultSections())`
- `TestReconcileHookFreshFile`, `TestReconcileHookMergesAndUpgrades`, `TestReconcileHookLeavesInvalidJSON`: every `writeReconcileHook(root, "SIDECAR.md")` → `writeReconcileHook(root, "SIDECAR.md", defaultSections())`

Then append new tests:

```go
// init_test.go (append)
func TestClaudeNoteCustomSections(t *testing.T) {
	secs := []Section{
		{"🧠", "Needs action", "for the human"},
		{"", "Todo", ""},
	}
	note := claudeNote("SIDECAR.md", secs)
	for _, want := range []string{
		claudeNoteMarker,
		"`## 🧠 Needs action` — for the human",
		"`## Todo`",
		"sidecar SIDECAR.md",
	} {
		if !strings.Contains(note, want) {
			t.Errorf("note missing %q:\n%s", want, note)
		}
	}
	if strings.Contains(note, "Todo` — ") {
		t.Errorf("hintless section should have no ' — meaning':\n%s", note)
	}
}

func TestReconcileMessageCustomSections(t *testing.T) {
	secs := []Section{{"🧠", "Needs action", ""}, {"✅", "Done", ""}}
	msg := reconcileMessage("SIDECAR.md", secs)
	if !strings.Contains(msg, "Sections: 🧠 Needs action / ✅ Done.") {
		t.Errorf("reconcile message section list wrong:\n%s", msg)
	}
	if !strings.Contains(msg, hookSentinel) {
		t.Errorf("reconcile message missing sentinel:\n%s", msg)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run 'TestClaudeNoteCustom|TestReconcileMessageCustom' -v`
Expected: FAIL — signatures don't match / functions still take one arg (compile error)

- [ ] **Step 3: Write minimal implementation**

In `init.go`, replace `claudeNote`:

```go
func claudeNote(rel string, sections []Section) string {
	var secLines strings.Builder
	for _, s := range sections {
		secLines.WriteString("`" + s.Header() + "`")
		if s.Hint != "" {
			secLines.WriteString(" — " + s.Hint)
		}
		secLines.WriteString("\n")
	}
	const tmpl = "<!-- sidecar:review-queue -->\n" +
		"## Review queue (sidecar)\n\n" +
		"Maintain `%[1]s` as a live review / TODO queue for the human. Sections:\n" +
		"%[2]s" +
		"Put bare URLs on their own line (keeps them clickable); keep entries short.\n\n" +
		"The human watches it live with `sidecar %[1]s`. If sidecar isn't installed:\n" +
		"`go install github.com/than/sidecar@latest`, or a prebuilt binary from\n" +
		"https://github.com/than/sidecar/releases/latest\n" +
		"<!-- /sidecar:review-queue -->\n"
	return fmt.Sprintf(tmpl, rel, secLines.String())
}
```

Change `writeClaudeNote` signature and its `claudeNote` call:

```go
func writeClaudeNote(root, rel string, sections []Section) {
	// ... unchanged body until the WriteString call ...
	// change: f.WriteString(prefix + claudeNote(rel, sections))
```

(Only two edits inside `writeClaudeNote`: the signature line, and `claudeNote(rel)` → `claudeNote(rel, sections)`.)

Change `offerClaudeHook` signature and its two writer calls:

```go
func offerClaudeHook(fileAbs string, sections []Section) {
	// ... unchanged until the switch ...
	case "c":
		writeClaudeNote(root, rel, sections)
	case "b":
		writeClaudeNote(root, rel, sections)
		writeReconcileHook(root, rel, sections)
	// ...
}
```

Replace `reconcileMessage`:

```go
func reconcileMessage(rel string, sections []Section) string {
	labels := make([]string, len(sections))
	for i, s := range sections {
		labels[i] = s.label()
	}
	return fmt.Sprintf("If your last turn changed task state, reconcile %s — %s the human watches with `sidecar %s`. Sections: %s.", rel, hookSentinel, rel, strings.Join(labels, " / "))
}
```

Change `reconcileHookEntry`:

```go
func reconcileHookEntry(rel string, sections []Section) map[string]any {
	return map[string]any{
		"hooks": []any{
			map[string]any{"type": "command", "command": "echo " + shSingleQuote(reconcileMessage(rel, sections))},
		},
	}
}
```

Change `writeReconcileHook` signature and its `reconcileHookEntry` call:

```go
func writeReconcileHook(root, rel string, sections []Section) {
	path := filepath.Join(root, ".claude", "settings.json")
	entry := reconcileHookEntry(rel, sections)
	// ... rest unchanged ...
}
```

In `runInit`, change `offerClaudeHook(abs)` → `offerClaudeHook(abs, defaultSections())` (Task 5 replaces `defaultSections()` with the picked set).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./...`
Expected: PASS (all existing init tests + the two new ones)

- [ ] **Step 5: Commit**

```bash
git add init.go init_test.go
git commit -m "feat: thread sections through CLAUDE.md note and reconcile hook (#3)"
```

---

### Task 4: The section picker (bubbletea)

**Files:**
- Create: `picker.go`
- Test: `picker_test.go`

**Interfaces:**
- Consumes: `Section`, `defaultSections()` (Task 1), the `style.go` color constants
- Produces:
  - `type picker struct { ... }` implementing `tea.Model`
  - `func newPicker(sections []Section) picker`
  - `func (p picker) result() []Section` — included, non-empty-name rows in order; `nil` if canceled
  - `func pickSections(sections []Section) []Section` — runs the program; on error/empty/cancel returns the input `sections`

- [ ] **Step 1: Write the failing test**

```go
// picker_test.go
package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func send(p picker, keys ...string) picker {
	for _, k := range keys {
		next, _ := p.Update(key(k))
		p = next.(picker)
	}
	return p
}

func TestPickerToggleExcludes(t *testing.T) {
	p := send(newPicker(defaultSections()), "space") // deselect row 0 (Needs action)
	got := p.result()
	if len(got) != 4 || got[0].Name != "In progress" {
		t.Fatalf("toggle didn't exclude row 0: %+v", got)
	}
}

func TestPickerReorderDown(t *testing.T) {
	p := send(newPicker(defaultSections()), "J") // move row 0 down past row 1
	got := p.result()
	if got[0].Name != "In progress" || got[1].Name != "Needs action" {
		t.Fatalf("J did not reorder: %+v", got[:2])
	}
	if p.cursor != 1 {
		t.Errorf("cursor should follow moved row, got %d", p.cursor)
	}
}

func TestPickerReorderBounds(t *testing.T) {
	p := send(newPicker(defaultSections()), "K") // already at top; no-op
	if p.result()[0].Name != "Needs action" {
		t.Errorf("K at top should be a no-op")
	}
}

func TestPickerDelete(t *testing.T) {
	p := send(newPicker(defaultSections()), "d")
	if len(p.result()) != 4 || p.result()[0].Name != "In progress" {
		t.Fatalf("delete row 0 failed: %+v", p.result())
	}
}

func TestPickerAddAndEdit(t *testing.T) {
	// a: insert blank after cursor and enter edit (emoji field).
	// type "★", enter -> name field, type "Blocked", enter -> hint field,
	// type "waiting", enter -> commit.
	p := newPicker(defaultSections())
	p = send(p, "a", "★", "enter", "Blocked", "enter", "waiting", "enter")
	got := p.result()
	var found *Section
	for i := range got {
		if got[i].Name == "Blocked" {
			found = &got[i]
		}
	}
	if found == nil {
		t.Fatalf("added section not present: %+v", got)
	}
	if found.Emoji != "★" || found.Hint != "waiting" {
		t.Errorf("added section fields wrong: %+v", *found)
	}
}

func TestPickerAddEmptyNameDropped(t *testing.T) {
	// a: add, then leave name empty -> row is dropped on commit.
	p := newPicker(defaultSections())
	p = send(p, "a", "enter", "enter", "enter") // empty emoji, empty name, empty hint
	if len(p.result()) != 5 {
		t.Errorf("empty-name add should be dropped, got %d rows", len(p.result()))
	}
}

func TestPickerEditExisting(t *testing.T) {
	// e on row 0: keep emoji (enter), rename to "Inbox" (enter), keep hint.
	p := newPicker(defaultSections())
	p = send(p, "e", "enter", "Inbox", "enter", "enter")
	if p.result()[0].Name != "Inbox" {
		t.Errorf("edit didn't rename row 0: %+v", p.result()[0])
	}
}

func TestPickerCancelReturnsNil(t *testing.T) {
	p := send(newPicker(defaultSections()), "space", "esc")
	if p.result() != nil {
		t.Errorf("cancel should return nil, got %+v", p.result())
	}
}

func TestPickerAcceptQuits(t *testing.T) {
	p := newPicker(defaultSections())
	_, cmd := p.Update(key("enter"))
	if cmd == nil {
		t.Error("enter should return a quit command")
	}
	if !p.done && !send(p, "enter").done {
		// done is set on the returned model
	}
}
```

**Edit-field convention (settled in Step 3):** each edit field starts EMPTY with the current value shown as `Placeholder`. Pressing Enter on an empty field keeps the old value; typing a value replaces it. This avoids the `textinput` append-to-prefill ambiguity, and is what `TestPickerEditExisting` relies on ("e", keep emoji, type "Inbox" for name, keep hint).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestPicker -v`
Expected: FAIL — `undefined: newPicker`

- [ ] **Step 3: Write minimal implementation**

```go
// picker.go
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type editField int

const (
	fieldNone editField = iota
	fieldEmoji
	fieldName
	fieldHint
)

type pickerRow struct {
	Section
	included bool
}

type picker struct {
	rows     []pickerRow
	cursor   int
	editing  editField
	input    textinput.Model
	done     bool
	canceled bool
}

func newPicker(sections []Section) picker {
	rows := make([]pickerRow, len(sections))
	for i, s := range sections {
		rows[i] = pickerRow{Section: s, included: true}
	}
	return picker{rows: rows, input: textinput.New()}
}

func (p picker) Init() tea.Cmd { return nil }

func (p picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}
	if p.editing != fieldNone {
		return p.updateEditing(km)
	}
	return p.updateNav(km)
}

func (p picker) updateNav(km tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch km.String() {
	case "j", "down":
		if p.cursor < len(p.rows)-1 {
			p.cursor++
		}
	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
		}
	case " ":
		if len(p.rows) > 0 {
			p.rows[p.cursor].included = !p.rows[p.cursor].included
		}
	case "J":
		if p.cursor < len(p.rows)-1 {
			p.rows[p.cursor], p.rows[p.cursor+1] = p.rows[p.cursor+1], p.rows[p.cursor]
			p.cursor++
		}
	case "K":
		if p.cursor > 0 {
			p.rows[p.cursor], p.rows[p.cursor-1] = p.rows[p.cursor-1], p.rows[p.cursor]
			p.cursor--
		}
	case "e":
		if len(p.rows) > 0 {
			p = p.beginEdit(fieldEmoji)
		}
	case "a":
		at := p.cursor + 1
		if len(p.rows) == 0 {
			at = 0
		}
		p.rows = append(p.rows, pickerRow{})
		copy(p.rows[at+1:], p.rows[at:])
		p.rows[at] = pickerRow{included: true}
		p.cursor = at
		p = p.beginEdit(fieldEmoji)
	case "d":
		if len(p.rows) > 0 {
			p.rows = append(p.rows[:p.cursor], p.rows[p.cursor+1:]...)
			if p.cursor >= len(p.rows) && p.cursor > 0 {
				p.cursor--
			}
		}
	case "enter":
		p.done = true
		return p, tea.Quit
	case "esc", "q", "ctrl+c":
		p.canceled = true
		return p, tea.Quit
	}
	return p, nil
}

// beginEdit focuses the text input for one field. The field starts empty with
// the current value shown as placeholder, so pressing Enter on an empty input
// keeps the old value and a typed value replaces it.
func (p picker) beginEdit(f editField) picker {
	p.editing = f
	ti := textinput.New()
	ti.Focus()
	switch f {
	case fieldEmoji:
		ti.Prompt = "emoji: "
		ti.Placeholder = p.rows[p.cursor].Emoji
	case fieldName:
		ti.Prompt = "name: "
		ti.Placeholder = p.rows[p.cursor].Name
	case fieldHint:
		ti.Prompt = "hint: "
		ti.Placeholder = p.rows[p.cursor].Hint
	}
	p.input = ti
	return p
}

func (p picker) updateEditing(km tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch km.Type {
	case tea.KeyEnter:
		val := strings.TrimSpace(p.input.Value())
		switch p.editing {
		case fieldEmoji:
			if val != "" {
				p.rows[p.cursor].Emoji = val
			}
			return p.beginEdit(fieldName), nil
		case fieldName:
			if val != "" {
				p.rows[p.cursor].Name = val
			}
			return p.beginEdit(fieldHint), nil
		case fieldHint:
			if val != "" {
				p.rows[p.cursor].Hint = val
			}
			return p.endEdit(), nil
		}
	case tea.KeyEsc:
		return p.endEdit(), nil
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(km)
	return p, cmd
}

// endEdit leaves edit mode and drops the current row if it ended up with no
// name (covers add-then-abandon).
func (p picker) endEdit() picker {
	p.editing = fieldNone
	p.input.Blur()
	if p.cursor < len(p.rows) && strings.TrimSpace(p.rows[p.cursor].Name) == "" {
		p.rows = append(p.rows[:p.cursor], p.rows[p.cursor+1:]...)
		if p.cursor >= len(p.rows) && p.cursor > 0 {
			p.cursor--
		}
	}
	return p
}

// result is the chosen sections: included rows with a non-empty name, in
// display order. Returns nil when the user canceled.
func (p picker) result() []Section {
	if p.canceled {
		return nil
	}
	var out []Section
	for _, r := range p.rows {
		if r.included && strings.TrimSpace(r.Name) != "" {
			out = append(out, r.Section)
		}
	}
	return out
}

var (
	pickerCursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorHeading)).Bold(true)
	pickerHintStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(colorStatusFg))
	pickerHelpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(colorStatusFg))
)

func (p picker) View() string {
	var b strings.Builder
	b.WriteString("  Customize your sidecar sections\n")
	b.WriteString("  " + pickerHelpStyle.Render("jk move · space toggle · J/K reorder · e edit · a add · d delete · ⏎ done") + "\n\n")
	for i, r := range p.rows {
		cursor := "   "
		if i == p.cursor {
			cursor = pickerCursorStyle.Render(" ▸ ")
		}
		box := "[ ]"
		if r.included {
			box = "[x]"
		}
		line := cursor + box + " " + r.Section.label()
		if r.Hint != "" {
			line += "  " + pickerHintStyle.Render("— "+r.Hint)
		}
		b.WriteString(line + "\n")
	}
	if p.editing != fieldNone {
		b.WriteString("\n  " + p.input.View() + "\n")
	}
	return b.String()
}

// pickSections runs the interactive picker. On any error, cancel, or an empty
// result it returns the input sections unchanged.
func pickSections(sections []Section) []Section {
	m, err := tea.NewProgram(newPicker(sections)).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "sidecar: section picker:", err)
		return sections
	}
	res := m.(picker).result()
	if len(res) == 0 {
		return sections
	}
	return res
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go mod tidy && go test ./... -run TestPicker -v`
Expected: PASS. (`go mod tidy` resolves `bubbles/textinput`; go.sum should be unchanged since `bubbles` is already required.)

- [ ] **Step 5: Commit**

```bash
git add picker.go picker_test.go go.mod go.sum
git commit -m "feat: interactive bubbletea section picker (#3)"
```

---

### Task 5: Wire the picker into init

**Files:**
- Modify: `init.go` — `runInit` and `offerCreate` call `pickSections` when interactive; thread the result into `scaffold` and `offerClaudeHook`
- Test: `init_test.go`

**Interfaces:**
- Consumes: `pickSections` (Task 4), `scaffold` (Task 2), `offerClaudeHook` (Task 3), `stdinIsTerminal()` (existing)

- [ ] **Step 1: Write the failing test**

```go
// init_test.go (append) — the non-interactive path must still write the
// default template and must NOT start a picker (tests aren't a TTY).
func TestInitNonInteractiveUsesDefaults(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "SIDECAR.md")
	if code := runInit([]string{target}); code != 0 {
		t.Fatalf("runInit exit = %d", code)
	}
	data, _ := os.ReadFile(target)
	for _, s := range defaultSections() {
		if !strings.Contains(string(data), s.Header()) {
			t.Errorf("default template missing %q:\n%s", s.Header(), data)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestInitNonInteractive -v`
Expected: PASS already if Task 2 left `defaultSections()` in `runInit` — this is a regression guard. If it FAILS, the wiring in Step 3 broke the non-interactive path; fix before proceeding.

- [ ] **Step 3: Write minimal implementation**

In `init.go` `runInit`, replace the create branch and the `offerClaudeHook` call so the picked sections flow through:

```go
func runInit(args []string) int {
	target := defaultFile
	if len(args) > 0 {
		target = args[0]
	}
	abs, err := filepath.Abs(expandTilde(target))
	if err != nil {
		fmt.Fprintln(os.Stderr, "sidecar init:", err)
		return 1
	}

	sections := defaultSections()
	if _, err := os.Stat(abs); err == nil {
		fmt.Printf("%s already exists — leaving it untouched.\n", target)
	} else {
		if stdinIsTerminal() {
			sections = pickSections(defaultSections())
		}
		if err := scaffold(abs, sections); err != nil {
			fmt.Fprintln(os.Stderr, "sidecar init:", err)
			return 1
		}
		fmt.Printf("Created %s\n", target)
	}

	offerGitExclude(abs)
	offerClaudeHook(abs, sections)

	fmt.Printf("\nWatch it:  sidecar %s\n", filepath.Base(abs))
	return 0
}
```

In `offerCreate`, replace the `default:` create branch so the picker runs before scaffolding:

```go
	default: // Enter or "y" → create
		sections := defaultSections()
		if stdinIsTerminal() {
			sections = pickSections(defaultSections())
		}
		if err := scaffold(abs, sections); err != nil {
			fmt.Fprintln(os.Stderr, "sidecar:", err)
			return
		}
		fmt.Printf("Created %s\n", filepath.Base(abs))
		offerGitExclude(abs)
		offerClaudeHook(abs, sections)
	}
```

(Note: `offerCreate` already checked `stdinIsTerminal()` at its top, so the inner guard is redundant there but harmless and keeps the two call sites identical. Keep it for symmetry.)

- [ ] **Step 4: Run the full suite**

Run: `go vet ./... && go test ./...`
Expected: PASS — every test green, `go vet` clean.

- [ ] **Step 5: Commit**

```bash
git add init.go init_test.go
git commit -m "feat: run section picker during init and create prompt (#3)"
```

---

## Manual smoke test (after Task 5)

Not automated (needs a TTY). Run once to confirm the picker works end to end:

```bash
go build -o /tmp/sidecar . && cd "$(mktemp -d)" && /tmp/sidecar init
```

Expected: the picker appears; toggle/reorder/edit/add work; on Enter, the created `SIDECAR.md` reflects your choices, and the CLAUDE.md prompt (if accepted) lists the same sections.

## Self-review notes

- Spec §"Three consumers" → Tasks 2 & 3 cover template, note, hook. ✓
- Spec §"picker" keys (jk/space/J/K/e/a/d/⏎/esc) → Task 4 `updateNav`. ✓
- Spec §"edit mode" (emoji→name→hint, empty name drops row, emoji/hint optional) → Task 4 `updateEditing`/`endEdit`, tests `TestPickerAddAndEdit`, `TestPickerAddEmptyNameDropped`. ✓
- Spec §"error handling" (piped→default, cancel→default, empty→default, error→default) → `pickSections` + Task 5 guard, `TestInitNonInteractiveUsesDefaults`, `TestPickerCancelReturnsNil`. ✓
- Spec §"out of scope" (no persistence, existing files untouched) → `scaffold`/`runInit` leave existing files alone; existing `TestInitLeavesExistingFile` still guards. ✓
- README emoji-width fix is tracked as follow-up, not in this plan. ✓
