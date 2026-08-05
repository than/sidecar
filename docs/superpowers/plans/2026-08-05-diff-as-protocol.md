# Diff as Protocol Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the static reconcile reminder with `sidecar diff` — a semantic diff of the board since last turn — and make `.sidecar/` sidecar's gitignored project home.

**Architecture:** A board parser (`board.go`) turns raw markdown into sections and items. A differ (`semdiff.go`) matches items across two board versions and emits one line per change, falling back to a zero-context unified diff when parsing fails. A new `diff` subcommand (`diffcmd.go`) wires this to a snapshot at `.sidecar/previous.md`. Init moves the board to `.sidecar/sidecar.md`, excludes the folder via `.git/info/exclude`, installs a guarded hook, and rewrites the CLAUDE.md note in place with new writing guidance.

**Tech Stack:** Go 1.x, stdlib only (no new dependencies). Existing test style: plain `testing`, table-free, `t.TempDir()`.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-08-04-diff-as-protocol-design.md`.
- Board file: `.sidecar/sidecar.md`. Snapshot: `.sidecar/previous.md`. Dir constant name: `sidecarDirName = ".sidecar"`.
- `sidecar diff` never exits non-zero in a hook context: missing board, missing snapshot, unchanged file all print nothing and exit 0.
- No shelling out for diffs — the unified fallback is computed in-process.
- All user-facing copy in Apple Developer documentation voice: declarative, front-loaded verb, present tense, outcomes not process.
- Hook fallback text must contain the sentinel phrase `the sidecar review queue` (hook upgrade detection depends on it).
- Commit messages end with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

---

### Task 1: Board parser

**Files:**
- Create: `board.go`
- Test: `board_test.go`

**Interfaces:**
- Produces: `type BoardItem struct { Key, Raw string }`, `type BoardSection struct { Label string; Items []BoardItem }`, `type Board struct { Sections []BoardSection }`, `func parseBoard(raw string) (Board, bool)`, `func normalizeItem(line string) string`, `const sidecarDirName = ".sidecar"`.
- `Key` is the normalized first line of the item (identity for matching). `Raw` is the full item text including continuation lines. `ok == false` means no `## ` headings were found — callers fall back to a textual diff.

- [ ] **Step 1: Write the failing tests**

```go
// board_test.go
package main

import "testing"

const sampleBoard = `# Sidecar

<!--
Sidecar review queue — agent: keep this current as you work.
· Move each item to the section matching its state.
-->

## 🧠 Needs action

- Review PR #7
  https://example.com/pr/7

## 🚧 In progress

- Fix the parser
- Ship v2

## ✅ Done

- nothing yet
`

func TestParseBoardSectionsAndItems(t *testing.T) {
	b, ok := parseBoard(sampleBoard)
	if !ok {
		t.Fatal("expected ok for a board with headings")
	}
	if len(b.Sections) != 3 {
		t.Fatalf("sections = %d, want 3", len(b.Sections))
	}
	if b.Sections[0].Label != "🧠 Needs action" {
		t.Errorf("label = %q", b.Sections[0].Label)
	}
	if len(b.Sections[1].Items) != 2 {
		t.Fatalf("in-progress items = %d, want 2", len(b.Sections[1].Items))
	}
	if b.Sections[1].Items[0].Key != "Fix the parser" {
		t.Errorf("key = %q", b.Sections[1].Items[0].Key)
	}
}

func TestParseBoardContinuationLines(t *testing.T) {
	b, _ := parseBoard(sampleBoard)
	raw := b.Sections[0].Items[0].Raw
	if want := "- Review PR #7\n  https://example.com/pr/7"; raw != want {
		t.Errorf("raw = %q, want %q", raw, want)
	}
	// Key is the first line only, bullet stripped, whitespace collapsed.
	if b.Sections[0].Items[0].Key != "Review PR #7" {
		t.Errorf("key = %q", b.Sections[0].Items[0].Key)
	}
}

func TestParseBoardCommentIgnored(t *testing.T) {
	b, _ := parseBoard(sampleBoard)
	for _, s := range b.Sections {
		for _, it := range s.Items {
			if it.Key == "· Move each item to the section matching its state." {
				t.Error("comment content parsed as an item")
			}
		}
	}
}

func TestParseBoardNoHeadings(t *testing.T) {
	if _, ok := parseBoard("just some prose\n- a stray bullet\n"); ok {
		t.Error("expected ok=false for a file with no ## headings")
	}
}

func TestNormalizeItem(t *testing.T) {
	if got := normalizeItem("-   Fix   the  parser  "); got != "Fix the parser" {
		t.Errorf("got %q", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestParseBoard|TestNormalizeItem' ./...`
Expected: FAIL — `undefined: parseBoard`.

- [ ] **Step 3: Implement board.go**

```go
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
		case strings.HasPrefix(strings.TrimLeft(ln, " \t"), "- ") && !strings.HasPrefix(ln, " "):
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run 'TestParseBoard|TestNormalizeItem' ./...`
Expected: PASS. Then `go test ./...` — everything else still green.

- [ ] **Step 5: Commit**

```bash
git add board.go board_test.go
git commit -m "feat: board parser — sections and items from raw markdown

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: Semantic differ

**Files:**
- Create: `semdiff.go`
- Test: `semdiff_test.go`

**Interfaces:**
- Consumes: `Board`, `BoardItem`, `BoardSection` from Task 1.
- Produces: `func semanticDiff(old, new Board) []string` (one output line per change, empty when no item-level changes), `func sectionTag(label string) string`, `func prefixMatch(a, b string) bool`.
- Output line formats, exactly: `moved 🚧→✅: "title"`, `added 🧠: "title"`, `removed 🧠: "title"`, `edited 🚘: "title"`. The tag is the section's emoji when its label starts with one, else the full label. Titles longer than 60 runes are cut to 59 plus `…`.

- [ ] **Step 1: Write the failing tests**

```go
// semdiff_test.go
package main

import (
	"strings"
	"testing"
)

func board(t *testing.T, raw string) Board {
	t.Helper()
	b, ok := parseBoard(raw)
	if !ok {
		t.Fatal("test board failed to parse")
	}
	return b
}

func TestSemanticDiffMoved(t *testing.T) {
	old := board(t, "## 🚧 In progress\n\n- Ship v2\n\n## ✅ Done\n\n- nothing yet\n")
	new := board(t, "## 🚧 In progress\n\n- nothing left\n\n## ✅ Done\n\n- Ship v2\n")
	out := semanticDiff(old, new)
	want := `moved 🚧→✅: "Ship v2"`
	if len(out) == 0 || out[0] != want {
		t.Fatalf("out = %q, want first line %q", out, want)
	}
}

func TestSemanticDiffAddedRemoved(t *testing.T) {
	old := board(t, "## 🧠 Needs action\n\n- Old idea\n")
	new := board(t, "## 🧠 Needs action\n\n- Fresh idea\n")
	out := strings.Join(semanticDiff(old, new), "\n")
	if !strings.Contains(out, `added 🧠: "Fresh idea"`) {
		t.Errorf("missing added line:\n%s", out)
	}
	if !strings.Contains(out, `removed 🧠: "Old idea"`) {
		t.Errorf("missing removed line:\n%s", out)
	}
}

func TestSemanticDiffEditedByPrefix(t *testing.T) {
	old := board(t, "## 🚘 Parked\n\n- Picker: allow clearing an emoji\n")
	new := board(t, "## 🚘 Parked\n\n- Picker: allow clearing an emoji or hint\n")
	out := semanticDiff(old, new)
	want := `edited 🚘: "Picker: allow clearing an emoji or hint"`
	if len(out) != 1 || out[0] != want {
		t.Fatalf("out = %q, want [%q]", out, want)
	}
}

func TestSemanticDiffContinuationEdit(t *testing.T) {
	old := board(t, "## 🧠 Needs action\n\n- Review PR #7\n  first note\n")
	new := board(t, "## 🧠 Needs action\n\n- Review PR #7\n  a different note\n")
	out := semanticDiff(old, new)
	want := `edited 🧠: "Review PR #7"`
	if len(out) != 1 || out[0] != want {
		t.Fatalf("out = %q, want [%q]", out, want)
	}
}

func TestSemanticDiffUnchanged(t *testing.T) {
	b := board(t, "## 🧠 Needs action\n\n- Same item\n")
	if out := semanticDiff(b, b); len(out) != 0 {
		t.Errorf("expected no output, got %q", out)
	}
}

func TestSemanticDiffTextOnlySectionTag(t *testing.T) {
	old := board(t, "## Todo\n\n- nothing yet\n")
	new := board(t, "## Todo\n\n- A task\n")
	out := strings.Join(semanticDiff(old, new), "\n")
	if !strings.Contains(out, `added Todo: "A task"`) {
		t.Errorf("text-only section tag wrong:\n%s", out)
	}
}

func TestSectionTag(t *testing.T) {
	if got := sectionTag("🧠 Needs action"); got != "🧠" {
		t.Errorf("emoji tag = %q", got)
	}
	if got := sectionTag("Todo"); got != "Todo" {
		t.Errorf("text tag = %q", got)
	}
}

func TestSemanticDiffLongTitleTruncated(t *testing.T) {
	long := strings.Repeat("x", 80)
	old := board(t, "## 🧠 Needs action\n\n- nothing yet\n")
	new := board(t, "## 🧠 Needs action\n\n- "+long+"\n")
	out := strings.Join(semanticDiff(old, new), "\n")
	if !strings.Contains(out, strings.Repeat("x", 59)+"…") || strings.Contains(out, strings.Repeat("x", 60)) {
		t.Errorf("title not truncated to 59+…:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestSemanticDiff|TestSectionTag' ./...`
Expected: FAIL — `undefined: semanticDiff`.

- [ ] **Step 3: Implement semdiff.go**

Matching rules, in order:
1. Exact `Key` match, any section: same section + same `Raw` → unchanged; same section + different `Raw` → edited; different section → moved.
2. Prefix match within the same section (both sides still unmatched) → edited. `prefixMatch` = common prefix ≥ 8 runes and ≥ half the shorter key.
3. Remainder: new-only → added, old-only → removed.

```go
// semdiff.go — one line per item change between two board versions.
package main

import (
	"fmt"
	"strings"
	"unicode"
)

type itemRef struct {
	section string
	item    BoardItem
	matched bool
}

// semanticDiff reports item-level changes from old to new, one line each:
// moved (highest signal), edited, added, removed. Empty when no item changed
// — a caller seeing raw bytes differ but no output should fall back to a
// textual diff.
func semanticDiff(old, new Board) []string {
	olds := flatten(old)
	news := flatten(new)

	byKey := map[string][]*itemRef{}
	for _, r := range olds {
		byKey[r.item.Key] = append(byKey[r.item.Key], r)
	}

	var moved, edited, added, removed []string

	// Pass 1: exact key matches.
	for _, n := range news {
		for _, o := range byKey[n.item.Key] {
			if o.matched {
				continue
			}
			o.matched, n.matched = true, true
			switch {
			case o.section != n.section:
				moved = append(moved, fmt.Sprintf("moved %s→%s: %q", sectionTag(o.section), sectionTag(n.section), title(n.item.Key)))
			case o.item.Raw != n.item.Raw:
				edited = append(edited, fmt.Sprintf("edited %s: %q", sectionTag(n.section), title(n.item.Key)))
			}
			break
		}
	}

	// Pass 2: prefix matches within the same section.
	for _, n := range news {
		if n.matched {
			continue
		}
		for _, o := range olds {
			if o.matched || o.section != n.section || !prefixMatch(o.item.Key, n.item.Key) {
				continue
			}
			o.matched, n.matched = true, true
			edited = append(edited, fmt.Sprintf("edited %s: %q", sectionTag(n.section), title(n.item.Key)))
			break
		}
	}

	for _, n := range news {
		if !n.matched {
			added = append(added, fmt.Sprintf("added %s: %q", sectionTag(n.section), title(n.item.Key)))
		}
	}
	for _, o := range olds {
		if !o.matched {
			removed = append(removed, fmt.Sprintf("removed %s: %q", sectionTag(o.section), title(o.item.Key)))
		}
	}

	out := append(moved, edited...)
	out = append(out, added...)
	return append(out, removed...)
}

func flatten(b Board) []*itemRef {
	var refs []*itemRef
	for _, s := range b.Sections {
		for _, it := range s.Items {
			refs = append(refs, &itemRef{section: s.Label, item: it})
		}
	}
	return refs
}

// sectionTag is the section's emoji when the label starts with one, else the
// whole label. "Starts with an emoji" ≈ first field's first rune is a symbol.
func sectionTag(label string) string {
	fields := strings.Fields(label)
	if len(fields) > 1 {
		r := []rune(fields[0])[0]
		if unicode.IsSymbol(r) || r > 0x2600 {
			return fields[0]
		}
	}
	return label
}

// prefixMatch reports whether two keys share enough of a prefix to be the
// same item after an edit: at least 8 runes and at least half of the shorter.
func prefixMatch(a, b string) bool {
	ar, br := []rune(a), []rune(b)
	short := len(ar)
	if len(br) < short {
		short = len(br)
	}
	p := 0
	for p < short && ar[p] == br[p] {
		p++
	}
	return p >= 8 && p*2 >= short
}

// title clamps a key to 60 runes for the output line.
func title(key string) string {
	r := []rune(key)
	if len(r) <= 60 {
		return key
	}
	return string(r[:59]) + "…"
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run 'TestSemanticDiff|TestSectionTag' ./...`
Expected: PASS. Then `go test ./...` still green.

- [ ] **Step 5: Commit**

```bash
git add semdiff.go semdiff_test.go
git commit -m "feat: semantic board differ — moved/edited/added/removed lines

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: Unified -U0 fallback and diffLines

**Files:**
- Modify: `semdiff.go`
- Test: `semdiff_test.go`

**Interfaces:**
- Consumes: `parseBoard`, `semanticDiff` (Tasks 1–2).
- Produces: `func unifiedU0(oldRaw, newRaw string) []string` (hunk headers + `-`/`+` lines, zero context), `func diffLines(oldRaw, newRaw string) []string` — semantic when both versions parse and item changes exist, else textual. `diffLines` is what the subcommand calls.

- [ ] **Step 1: Write the failing tests**

```go
// append to semdiff_test.go

func TestUnifiedU0(t *testing.T) {
	out := unifiedU0("a\nb\nc\n", "a\nX\nc\n")
	want := []string{"@@ -2 +2 @@", "-b", "+X"}
	if len(out) != len(want) {
		t.Fatalf("out = %q, want %q", out, want)
	}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, out[i], want[i])
		}
	}
}

func TestUnifiedU0Insert(t *testing.T) {
	out := strings.Join(unifiedU0("a\nc\n", "a\nb\nc\n"), "\n")
	if !strings.Contains(out, "+b") {
		t.Errorf("missing +b:\n%s", out)
	}
}

func TestDiffLinesSemanticWhenParsable(t *testing.T) {
	oldRaw := "## 🧠 Needs action\n\n- Old idea\n"
	newRaw := "## 🧠 Needs action\n\n- New idea\n"
	out := strings.Join(diffLines(oldRaw, newRaw), "\n")
	if !strings.Contains(out, "added 🧠:") || strings.Contains(out, "@@") {
		t.Errorf("expected semantic output:\n%s", out)
	}
}

func TestDiffLinesFallbackWhenUnparsable(t *testing.T) {
	out := strings.Join(diffLines("plain old\n", "plain new\n"), "\n")
	if !strings.Contains(out, "-plain old") || !strings.Contains(out, "+plain new") {
		t.Errorf("expected textual fallback:\n%s", out)
	}
}

func TestDiffLinesFallbackWhenNoItemChanges(t *testing.T) {
	// Both parse, but only the title changed — no item events, so fall back.
	oldRaw := "# One\n\n## 🧠 Needs action\n\n- Same\n"
	newRaw := "# Two\n\n## 🧠 Needs action\n\n- Same\n"
	out := strings.Join(diffLines(oldRaw, newRaw), "\n")
	if !strings.Contains(out, "-# One") {
		t.Errorf("expected textual fallback for non-item change:\n%s", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestUnifiedU0|TestDiffLines' ./...`
Expected: FAIL — `undefined: unifiedU0`.

- [ ] **Step 3: Implement in semdiff.go**

```go
// diffLines is the diff the hook prints: semantic when both versions parse
// into sections and at least one item changed; a zero-context unified diff
// otherwise. Callers only invoke it when the raw bytes differ.
func diffLines(oldRaw, newRaw string) []string {
	ob, ok1 := parseBoard(oldRaw)
	nb, ok2 := parseBoard(newRaw)
	if ok1 && ok2 {
		if out := semanticDiff(ob, nb); len(out) > 0 {
			return out
		}
	}
	return unifiedU0(oldRaw, newRaw)
}

// unifiedU0 is a minimal unified diff with zero context lines: "@@" hunk
// headers plus -/+ lines only. Computed in-process — no shelling out.
func unifiedU0(oldRaw, newRaw string) []string {
	o := strings.Split(strings.TrimSuffix(oldRaw, "\n"), "\n")
	n := strings.Split(strings.TrimSuffix(newRaw, "\n"), "\n")

	// LCS table, same shape as changedLines in diff.go.
	lcs := make([][]int32, len(o)+1)
	for i := range lcs {
		lcs[i] = make([]int32, len(n)+1)
	}
	for i := len(o) - 1; i >= 0; i-- {
		for j := len(n) - 1; j >= 0; j-- {
			if o[i] == n[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	type op struct {
		kind byte // '-' or '+'
		text string
		oi   int // 1-based old line for '-', old position for '+'
		ni   int // 1-based new line
	}
	var ops []op
	i, j := 0, 0
	for i < len(o) || j < len(n) {
		switch {
		case i < len(o) && j < len(n) && o[i] == n[j]:
			i++
			j++
		case j < len(n) && (i == len(o) || lcs[i][j+1] >= lcs[i+1][j]):
			ops = append(ops, op{'+', n[j], i, j + 1})
			j++
		default:
			ops = append(ops, op{'-', o[i], i + 1, j})
			i++
		}
	}

	// Group consecutive ops into hunks and render headers.
	var out []string
	for k := 0; k < len(ops); {
		start := k
		for k+1 < len(ops) {
			cur, next := ops[k], ops[k+1]
			adjacent := (next.oi <= cur.oi+1) && (next.ni <= cur.ni+1)
			if !adjacent {
				break
			}
			k++
		}
		k++
		hunk := ops[start:k]
		var dels, adds []string
		firstO, firstN := 0, 0
		for idx, opv := range hunk {
			if idx == 0 {
				firstO, firstN = opv.oi, opv.ni
			}
			if opv.kind == '-' {
				dels = append(dels, "-"+opv.text)
			} else {
				adds = append(adds, "+"+opv.text)
			}
		}
		out = append(out, hunkHeader(firstO, len(dels), firstN, len(adds)))
		out = append(out, dels...)
		out = append(out, adds...)
	}
	return out
}

// hunkHeader renders "@@ -a[,b] +c[,d] @@" in -U0 form: the count is omitted
// when it is 1, and a zero-count side keeps the position of the line before.
func hunkHeader(o, dels, n, adds int) string {
	side := func(pos, count int) string {
		if count == 0 {
			return fmt.Sprintf("%d,0", pos)
		}
		if count == 1 {
			return fmt.Sprintf("%d", pos)
		}
		return fmt.Sprintf("%d,%d", pos, count)
	}
	return fmt.Sprintf("@@ -%s +%s @@", side(o, dels), side(n, adds))
}
```

Note for the `+`-only hunk: `op.oi` for an insert is the old position *after which* the insert lands, which is what `-U0` headers use (`@@ -1,0 +2 @@`). The `TestUnifiedU0Insert` assertion only checks the `+b` payload; if the header math fights you, match GNU `diff -U0 old new` output for the same inputs and adjust `side()` — the agent-facing payload is the `-`/`+` lines.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run 'TestUnifiedU0|TestDiffLines' ./...`
Expected: PASS. Then `go test ./...` still green.

- [ ] **Step 5: Commit**

```bash
git add semdiff.go semdiff_test.go
git commit -m "feat: diffLines — semantic diff with unified -U0 fallback

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: `sidecar diff` subcommand and default board path

**Files:**
- Create: `diffcmd.go`
- Modify: `main.go` (subcommand dispatch, help text, viewer default path), `init.go:157` (`reconcileMessage` gains a label-based variant)
- Test: `diffcmd_test.go`

**Interfaces:**
- Consumes: `diffLines` (Task 3), `parseBoard` (Task 1), `reconcileMessageLabels` (added here), `expandTilde` (main.go).
- Produces: `func runDiff(args []string) int`, `func defaultBoardPath() string`, `func snapshotPath(boardAbs string) string`, `func reconcileMessageLabels(rel string, labels []string) string`. Tasks 5–6 rely on `defaultBoardPath` and `sidecarDirName`.

Behavior (from the spec):
- `sidecar diff` → default board; `sidecar diff path/to/file.md` → that file.
- Default board resolution, used by `diff`, the viewer, and `--static`: `.sidecar/sidecar.md` when it exists; else `SIDECAR.md` when it exists (legacy layout); else `.sidecar/sidecar.md` (the new-install target).
- Snapshot: `.sidecar/previous.md` for the default board; `<dir>/.sidecar/previous-<fnv32a-of-abs-path>.md` for an explicit path.
- Missing board / first run / unchanged → print nothing, exit 0. Changes → `diffLines` output, then the reconcile reminder line built from the current board's own headings, then snapshot overwrite.

- [ ] **Step 1: Write the failing tests**

```go
// diffcmd_test.go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withWorkDir runs f inside dir so relative default-path logic is exercised.
func withWorkDir(t *testing.T, dir string, f func()) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	f()
}

// captureStdout runs f and returns everything it printed.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	f()
	w.Close()
	os.Stdout = old
	buf := make([]byte, 1<<16)
	n, _ := r.Read(buf)
	return string(buf[:n])
}

const diffBoardV1 = "## 🧠 Needs action\n\n- Review PR #7\n\n## ✅ Done\n\n- nothing yet\n"
const diffBoardV2 = "## 🧠 Needs action\n\n- nothing left\n\n## ✅ Done\n\n- Review PR #7\n"

func TestRunDiffFirstRunSilent(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, sidecarDirName), 0o755)
	os.WriteFile(filepath.Join(dir, sidecarDirName, "sidecar.md"), []byte(diffBoardV1), 0o644)
	withWorkDir(t, dir, func() {
		out := captureStdout(t, func() {
			if code := runDiff(nil); code != 0 {
				t.Errorf("exit = %d", code)
			}
		})
		if out != "" {
			t.Errorf("first run printed %q", out)
		}
	})
	if _, err := os.Stat(filepath.Join(dir, sidecarDirName, "previous.md")); err != nil {
		t.Error("snapshot not written on first run")
	}
}

func TestRunDiffReportsMoveThenGoesSilent(t *testing.T) {
	dir := t.TempDir()
	board := filepath.Join(dir, sidecarDirName, "sidecar.md")
	os.MkdirAll(filepath.Join(dir, sidecarDirName), 0o755)
	os.WriteFile(board, []byte(diffBoardV1), 0o644)
	withWorkDir(t, dir, func() {
		captureStdout(t, func() { runDiff(nil) }) // first run seeds snapshot
		os.WriteFile(board, []byte(diffBoardV2), 0o644)
		out := captureStdout(t, func() { runDiff(nil) })
		if !strings.Contains(out, `moved 🧠→✅: "Review PR #7"`) {
			t.Errorf("missing move line:\n%s", out)
		}
		if !strings.Contains(out, "the sidecar review queue") {
			t.Errorf("missing closing reminder:\n%s", out)
		}
		// Snapshot advanced: an immediate re-run prints nothing.
		if again := captureStdout(t, func() { runDiff(nil) }); again != "" {
			t.Errorf("second run printed %q", again)
		}
	})
}

func TestRunDiffMissingBoardSilent(t *testing.T) {
	withWorkDir(t, t.TempDir(), func() {
		out := captureStdout(t, func() {
			if code := runDiff(nil); code != 0 {
				t.Errorf("exit = %d", code)
			}
		})
		if out != "" {
			t.Errorf("printed %q for a missing board", out)
		}
	})
}

func TestRunDiffExplicitPathKeyedSnapshot(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "NOTES.md")
	os.WriteFile(file, []byte(diffBoardV1), 0o644)
	withWorkDir(t, dir, func() {
		captureStdout(t, func() { runDiff([]string{"NOTES.md"}) })
	})
	matches, _ := filepath.Glob(filepath.Join(dir, sidecarDirName, "previous-*.md"))
	if len(matches) != 1 {
		t.Fatalf("keyed snapshot files = %v, want exactly 1", matches)
	}
}

func TestDefaultBoardPathPrefersSidecarDir(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, sidecarDirName), 0o755)
	os.WriteFile(filepath.Join(dir, sidecarDirName, "sidecar.md"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "SIDECAR.md"), []byte("y"), 0o644)
	withWorkDir(t, dir, func() {
		if got := defaultBoardPath(); got != filepath.Join(sidecarDirName, "sidecar.md") {
			t.Errorf("got %q", got)
		}
	})
}

func TestDefaultBoardPathLegacyFallback(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "SIDECAR.md"), []byte("y"), 0o644)
	withWorkDir(t, dir, func() {
		if got := defaultBoardPath(); got != "SIDECAR.md" {
			t.Errorf("got %q", got)
		}
	})
}

func TestDefaultBoardPathFreshDefault(t *testing.T) {
	withWorkDir(t, t.TempDir(), func() {
		if got := defaultBoardPath(); got != filepath.Join(sidecarDirName, "sidecar.md") {
			t.Errorf("got %q", got)
		}
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestRunDiff|TestDefaultBoardPath' ./...`
Expected: FAIL — `undefined: runDiff`.

- [ ] **Step 3: Implement diffcmd.go, wire main.go, add reconcileMessageLabels**

```go
// diffcmd.go — the `sidecar diff` subcommand: print board changes since the
// last run, then advance the snapshot. Built for hook use — it never exits
// non-zero for an absent board, absent snapshot, or unchanged file.
package main

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
)

func runDiff(args []string) int {
	path := defaultBoardPath()
	if len(args) > 0 {
		path = args[0]
	}
	abs, err := filepath.Abs(expandTilde(path))
	if err != nil {
		fmt.Fprintln(os.Stderr, "sidecar diff:", err)
		return 1
	}

	raw, err := os.ReadFile(abs)
	if err != nil {
		return 0 // no board — silent, never an error in a hook
	}
	snap := snapshotPath(abs)
	prev, err := os.ReadFile(snap)
	if err != nil {
		writeSnapshot(snap, raw) // first run — seed silently
		return 0
	}
	if bytes.Equal(prev, raw) {
		return 0
	}

	for _, line := range diffLines(string(prev), string(raw)) {
		fmt.Println(line)
	}
	fmt.Println(closingReminder(path, string(raw)))
	writeSnapshot(snap, raw)
	return 0
}

// defaultBoardPath resolves the board for bare invocations: the .sidecar/
// home when present, the legacy root file when that's all there is, and the
// .sidecar/ home again as the target for fresh setups.
func defaultBoardPath() string {
	home := filepath.Join(sidecarDirName, "sidecar.md")
	if _, err := os.Stat(home); err == nil {
		return home
	}
	if _, err := os.Stat(defaultFile); err == nil {
		return defaultFile
	}
	return home
}

// snapshotPath is .sidecar/previous.md for the standard board, and a
// path-keyed previous-<hash>.md beside it for explicit board files.
func snapshotPath(boardAbs string) string {
	dir := filepath.Dir(boardAbs)
	if filepath.Base(dir) == sidecarDirName && filepath.Base(boardAbs) == "sidecar.md" {
		return filepath.Join(dir, "previous.md")
	}
	h := fnv.New32a()
	h.Write([]byte(boardAbs))
	return filepath.Join(dir, sidecarDirName, fmt.Sprintf("previous-%08x.md", h.Sum32()))
}

func writeSnapshot(path string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}

// closingReminder is the last line of a non-empty diff: the reconcile
// reminder, with section labels read from the board itself so custom
// sections stay accurate.
func closingReminder(rel, raw string) string {
	var labels []string
	if b, ok := parseBoard(raw); ok {
		for _, s := range b.Sections {
			labels = append(labels, s.Label)
		}
	}
	return reconcileMessageLabels(rel, labels)
}
```

In `init.go`, refactor `reconcileMessage` (init.go:157) so both callers share one renderer:

```go
// reconcileMessageLabels renders the per-turn reminder from plain heading
// labels. reconcileMessage adapts []Section to it.
func reconcileMessageLabels(rel string, labels []string) string {
	return fmt.Sprintf("If your last turn changed task state, reconcile %s — %s the human watches with `sidecar %s`. Sections: %s.", rel, hookSentinel, rel, strings.Join(labels, " / "))
}

func reconcileMessage(rel string, sections []Section) string {
	labels := make([]string, len(sections))
	for i, s := range sections {
		labels[i] = s.label()
	}
	return reconcileMessageLabels(rel, labels)
}
```

In `main.go`:
1. Add to the subcommand switch (after `case "init":`): `case "diff": os.Exit(runDiff(os.Args[2:]))`.
2. Viewer mode: replace `path := defaultFile` with `path := defaultBoardPath()` (main.go:53). Same in `runStatic` (main.go:93).
3. Help text — replace the usage block:

```
usage: sidecar [file.md]         (default: .sidecar/sidecar.md)
       sidecar init [file.md]    create the board and wire it into Claude Code
       sidecar diff [file.md]    print board changes since the last run
       sidecar --static [file]   render once to stdout and exit (no TUI)
       sidecar --no-flash [file] disable the subtle change-flash (▸ still shows)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run 'TestRunDiff|TestDefaultBoardPath' ./...` then `go test ./...`
Expected: PASS, full suite green. Also smoke it: `go build -o /tmp/sidecar-dev . && cd $(mktemp -d) && mkdir .sidecar && printf '## 🧠 Needs action\n\n- hi\n' > .sidecar/sidecar.md && /tmp/sidecar-dev diff && printf '## 🧠 Needs action\n\n- hi there\n' > .sidecar/sidecar.md && /tmp/sidecar-dev diff` — second diff prints an `edited` line plus the reminder.

- [ ] **Step 5: Commit**

```bash
git add diffcmd.go diffcmd_test.go main.go init.go
git commit -m "feat: sidecar diff subcommand + .sidecar/ default board path

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: Init — .sidecar/ home, auto-exclude, guarded hook, note rewrite

**Files:**
- Modify: `init.go` (target default, exclude flow, hook command, CLAUDE.md note), `sections.go:44` (template heading)
- Test: `init_test.go`

**Interfaces:**
- Consumes: `defaultBoardPath`, `sidecarDirName` (Task 4), `appendLine`, `git`, `shSingleQuote`, `mergeReconcileHook` (existing init.go).
- Produces: `func excludeSidecarDir(dir string)`, `func replaceClaudeNote(existing, note string) (string, bool)`, updated `claudeNote(rel string, sections []Section) string`, updated `reconcileHookEntry` emitting the guarded command. Task 6 relies on all of these.

Changes:
1. `runInit` default target becomes `.sidecar/sidecar.md` (`filepath.Join(sidecarDirName, "sidecar.md")` instead of `defaultFile` at init.go:18); `scaffold` gains `os.MkdirAll(filepath.Dir(abs), 0o755)` before writing.
2. Default-path installs call `excludeSidecarDir` automatically — no prompt. `offerGitExclude` remains only for explicit custom paths outside `.sidecar/`.
3. The hook command becomes guarded: `command -v sidecar >/dev/null 2>&1 && sidecar diff || echo '<reminder>'` (reminder still carries the sentinel, so upgrade detection keeps working). When the board is not the default, `sidecar diff` gets the quoted rel path.
4. The wiring prompt defaults to note + hook: `Choice [B/c/n]`, Enter → `b`.
5. `claudeNote` gets the new template below; `writeClaudeNote` replaces the content between the `<!-- sidecar:review-queue -->` markers when present instead of skipping.

New `claudeNote` template (verbatim; `%[1]s` = rel path, `%[2]s` = section list):

```
<!-- sidecar:review-queue -->
## Sidecar board

Maintain `%[1]s` — the live board the human watches with `sidecar`.
Move each item to the section that matches its state:

%[2]s
Write entries in Apple Developer documentation voice: declarative,
front-loaded verb, present tense, one fact per sentence. State outcomes,
not process.

One entry is at most:
- a status tag and title on the first line
- two sentences of detail — more belongs in the PR or issue you link
- bare URLs, each on its own line
- one `Next:` line naming the single next action (optional)

If sidecar isn't installed: `go install github.com/than/sidecar@latest`,
or a prebuilt binary from https://github.com/than/sidecar/releases/latest
<!-- /sidecar:review-queue -->
```

- [ ] **Step 1: Write the failing tests**

```go
// append to init_test.go
// (uses the existing test helpers/patterns in init_test.go; git-dependent
// tests init a repo in t.TempDir() with exec.Command("git", "init"))

func TestExcludeSidecarDir(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	excludeSidecarDir(dir)
	data, err := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if err != nil || !strings.Contains(string(data), ".sidecar/") {
		t.Fatalf("info/exclude = %q, err %v", data, err)
	}
	// Idempotent: a second call adds nothing.
	excludeSidecarDir(dir)
	again, _ := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if strings.Count(string(again), ".sidecar/") != 1 {
		t.Errorf("exclude entry duplicated:\n%s", again)
	}
}

func TestReconcileHookEntryGuarded(t *testing.T) {
	entry := reconcileHookEntry(filepath.Join(sidecarDirName, "sidecar.md"), defaultSections())
	cmd := entry["hooks"].([]any)[0].(map[string]any)["command"].(string)
	if !strings.Contains(cmd, "command -v sidecar") || !strings.Contains(cmd, "sidecar diff") {
		t.Errorf("hook not guarded: %q", cmd)
	}
	if !strings.Contains(cmd, hookSentinel) {
		t.Errorf("hook fallback lost the sentinel: %q", cmd)
	}
	if strings.Contains(cmd, "sidecar diff '") {
		t.Errorf("default board should not pass an explicit path: %q", cmd)
	}
}

func TestReconcileHookEntryCustomPathPassed(t *testing.T) {
	entry := reconcileHookEntry("NOTES.md", defaultSections())
	cmd := entry["hooks"].([]any)[0].(map[string]any)["command"].(string)
	if !strings.Contains(cmd, "sidecar diff 'NOTES.md'") {
		t.Errorf("custom path missing from hook: %q", cmd)
	}
}

func TestReplaceClaudeNote(t *testing.T) {
	old := "# My project\n\n<!-- sidecar:review-queue -->\nold sidecar text\n<!-- /sidecar:review-queue -->\n\n## Other section\n"
	note := claudeNote(filepath.Join(sidecarDirName, "sidecar.md"), defaultSections())
	got, replaced := replaceClaudeNote(old, note)
	if !replaced {
		t.Fatal("expected replacement")
	}
	if strings.Contains(got, "old sidecar text") {
		t.Error("stale note survived")
	}
	if !strings.Contains(got, ".sidecar/sidecar.md") || !strings.Contains(got, "# My project") || !strings.Contains(got, "## Other section") {
		t.Errorf("replacement damaged surrounding content:\n%s", got)
	}
}

func TestReplaceClaudeNoteNoMarker(t *testing.T) {
	if _, replaced := replaceClaudeNote("# Plain file\n", "note"); replaced {
		t.Error("replaced without a marker")
	}
}

func TestClaudeNoteWritingRules(t *testing.T) {
	note := claudeNote(filepath.Join(sidecarDirName, "sidecar.md"), defaultSections())
	for _, want := range []string{"Apple Developer documentation voice", "two sentences of detail", "bare URLs, each on its own line", "`Next:` line"} {
		if !strings.Contains(note, want) {
			t.Errorf("note missing %q", want)
		}
	}
}
```

Add the small helper the tests use if `init_test.go` doesn't already have one:

```go
func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestExcludeSidecarDir|TestReconcileHookEntry|TestReplaceClaudeNote|TestClaudeNoteWritingRules' ./...`
Expected: FAIL — `undefined: excludeSidecarDir`, guarded-command assertions fail.

- [ ] **Step 3: Implement**

```go
// excludeSidecarDir appends ".sidecar/" to the repo's .git/info/exclude —
// local and uncommitted, so the repo never learns sidecar exists. No-op
// outside a work tree or when the entry is already ignored.
func excludeSidecarDir(dir string) {
	if out, ok := git(dir, "rev-parse", "--is-inside-work-tree"); !ok || out != "true" {
		return
	}
	if _, ignored := git(dir, "check-ignore", "-q", filepath.Join(dir, sidecarDirName)); ignored {
		return
	}
	path, ok := git(dir, "rev-parse", "--git-path", "info/exclude")
	if !ok {
		return
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	if err := appendLine(path, sidecarDirName+"/"); err != nil {
		fmt.Fprintln(os.Stderr, "sidecar init:", err)
	}
}
```

`reconcileHookEntry` becomes:

```go
// reconcileHookEntry runs `sidecar diff` when the binary is installed and
// falls back to the static reminder otherwise — the reminder keeps the
// sentinel phrase, so re-running init still finds and upgrades this hook.
func reconcileHookEntry(rel string, sections []Section) map[string]any {
	diffCmd := "sidecar diff"
	if rel != filepath.Join(sidecarDirName, "sidecar.md") {
		diffCmd += " " + shSingleQuote(rel)
	}
	cmd := "command -v sidecar >/dev/null 2>&1 && " + diffCmd + " || echo " + shSingleQuote(reconcileMessage(rel, sections))
	return map[string]any{
		"hooks": []any{
			map[string]any{"type": "command", "command": cmd},
		},
	}
}
```

`replaceClaudeNote` and the `writeClaudeNote` change:

```go
const claudeNoteEndMarker = "<!-- /sidecar:review-queue -->"

// replaceClaudeNote swaps the content between the sidecar markers for note.
// replaced is false when the file has no complete marker pair.
func replaceClaudeNote(existing, note string) (string, bool) {
	start := strings.Index(existing, "<!-- "+claudeNoteMarker+" -->")
	if start < 0 {
		return existing, false
	}
	end := strings.Index(existing[start:], claudeNoteEndMarker)
	if end < 0 {
		return existing, false
	}
	end = start + end + len(claudeNoteEndMarker)
	return existing[:start] + strings.TrimSuffix(note, "\n") + existing[end:], true
}
```

In `writeClaudeNote` (init.go:122), replace the early-return skip:

```go
	if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), claudeNoteMarker) {
		if updated, ok := replaceClaudeNote(string(data), claudeNote(rel, sections)); ok {
			if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
				fmt.Fprintln(os.Stderr, "sidecar init:", err)
				return
			}
			fmt.Println("Updated the sidecar note in CLAUDE.md")
			return
		}
		fmt.Println("CLAUDE.md has a sidecar marker but no closing marker — update it by hand.")
		return
	}
```

`claudeNote` (init.go:66) gets the new template from this task's header — same `%[1]s`/`%[2]s` slots, section list format unchanged. The wiring prompt (init.go:105) becomes:

```go
	fmt.Print(`
Help Claude keep this board updated?
  [b] CLAUDE.md note + per-turn diff hook (recommended)
  [c] CLAUDE.md note only
  [n] no
Choice [B/c/n]: `)
	switch readChoice() {
	case "c":
		writeClaudeNote(root, rel, sections)
	case "n":
		return
	default: // Enter or "b" — the hook is the product
		writeClaudeNote(root, rel, sections)
		writeReconcileHook(root, rel, sections)
	}
```

`runInit` (init.go:17): default target `filepath.Join(sidecarDirName, "sidecar.md")`; after scaffold, when the target's parent dir is `.sidecar`, call `excludeSidecarDir(root)` (resolve root via `git rev-parse --show-toplevel` from the board's dir, falling back to the board's parent's parent) instead of `offerGitExclude(abs)`; keep `offerGitExclude` for explicit non-`.sidecar` targets. `scaffold` (init.go:304) adds `os.MkdirAll(filepath.Dir(abs), 0o755)` before `os.WriteFile`. In `sections.go:46`, the starter title `# Sidecar` and comment header stay as-is (the template is already board-shaped).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run 'TestExcludeSidecarDir|TestReconcileHookEntry|TestReplaceClaudeNote|TestClaudeNoteWritingRules' ./...` then `go test ./...`
Expected: PASS; the full suite green (existing init tests that assert the old echo-only hook or old note text will need their expectations updated to the guarded command and new template — update assertions, not behavior).

- [ ] **Step 5: Commit**

```bash
git add init.go init_test.go sections.go
git commit -m "feat: init targets .sidecar/, auto-excludes it, installs guarded diff hook, rewrites note in place

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 6: Init — migration, --yes, docs

**Files:**
- Modify: `init.go` (migration + `--yes`), `main.go` (help), `README.md` (paths and diff subcommand)
- Test: `init_test.go`

**Interfaces:**
- Consumes: everything from Task 5, `defaultBoardPath` (Task 4).
- Produces: `func migrateLegacyBoard(root string, assumeYes bool) bool` (true when a legacy root SIDECAR.md was moved), `--yes`/`-y` handling in `runInit`.

Behavior:
- Migration runs when the init target is the default board, `.sidecar/sidecar.md` doesn't exist, and `<root>/SIDECAR.md` does. Interactive: `Move SIDECAR.md into .sidecar/? [Y/n]`. `--yes`: migrate without asking. On accept: `os.MkdirAll`, `os.Rename`, then `git rm --cached --quiet SIDECAR.md` only when `git ls-files --error-unmatch SIDECAR.md` succeeds (some setups only exclude it locally).
- `--yes` also: skips the section picker (defaults), skips the exclude prompt path entirely (auto-exclude already prompt-free), and installs note + hook without asking. Works with piped stdin — the flag, not TTY state, decides.

- [ ] **Step 1: Write the failing tests**

```go
// append to init_test.go

func TestMigrateLegacyBoard(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	os.WriteFile(filepath.Join(dir, "SIDECAR.md"), []byte("## 🧠 Needs action\n\n- carry me over\n"), 0o644)
	mustRun(t, dir, "git", "add", "SIDECAR.md")

	if !migrateLegacyBoard(dir, true) {
		t.Fatal("expected migration")
	}
	data, err := os.ReadFile(filepath.Join(dir, sidecarDirName, "sidecar.md"))
	if err != nil || !strings.Contains(string(data), "carry me over") {
		t.Fatalf("board content lost: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "SIDECAR.md")); !os.IsNotExist(err) {
		t.Error("legacy file still present")
	}
	// No longer tracked.
	cmd := exec.Command("git", "ls-files", "--error-unmatch", "SIDECAR.md")
	cmd.Dir = dir
	if cmd.Run() == nil {
		t.Error("SIDECAR.md still tracked after migration")
	}
}

func TestMigrateLegacyBoardUntracked(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	os.WriteFile(filepath.Join(dir, "SIDECAR.md"), []byte("## 🧠 Needs action\n\n- local only\n"), 0o644)
	if !migrateLegacyBoard(dir, true) {
		t.Fatal("expected migration of an untracked board")
	}
	if _, err := os.Stat(filepath.Join(dir, sidecarDirName, "sidecar.md")); err != nil {
		t.Error("board not moved")
	}
}

func TestMigrateLegacyBoardNothingToDo(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	if migrateLegacyBoard(dir, true) {
		t.Error("migrated with no legacy file present")
	}
}

func TestRunInitYesNonInteractive(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	withWorkDir(t, dir, func() {
		if code := runInit([]string{"--yes"}); code != 0 {
			t.Fatalf("exit = %d", code)
		}
	})
	if _, err := os.Stat(filepath.Join(dir, sidecarDirName, "sidecar.md")); err != nil {
		t.Error("board not created")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude")); !strings.Contains(string(data), ".sidecar/") {
		t.Error(".sidecar/ not excluded")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md")); !strings.Contains(string(data), "sidecar:review-queue") {
		t.Error("CLAUDE.md note not written")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, ".claude", "settings.json")); !strings.Contains(string(data), "sidecar diff") {
		t.Error("hook not written")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestMigrateLegacyBoard|TestRunInitYes' ./...`
Expected: FAIL — `undefined: migrateLegacyBoard`, `--yes` unrecognized.

- [ ] **Step 3: Implement**

```go
// migrateLegacyBoard moves a root-level SIDECAR.md into .sidecar/sidecar.md
// and untracks it when git knows it. assumeYes skips the prompt. Returns
// true when a move happened.
func migrateLegacyBoard(root string, assumeYes bool) bool {
	legacy := filepath.Join(root, defaultFile)
	target := filepath.Join(root, sidecarDirName, "sidecar.md")
	if _, err := os.Stat(legacy); err != nil {
		return false
	}
	if _, err := os.Stat(target); err == nil {
		return false // new home already populated — leave both alone
	}
	if !assumeYes {
		fmt.Printf("Move %s into %s/? [Y/n]: ", defaultFile, sidecarDirName)
		if c := readChoice(); c == "n" || c == "no" {
			return false
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "sidecar init:", err)
		return false
	}
	if err := os.Rename(legacy, target); err != nil {
		fmt.Fprintln(os.Stderr, "sidecar init:", err)
		return false
	}
	if _, tracked := git(root, "ls-files", "--error-unmatch", defaultFile); tracked {
		git(root, "rm", "--cached", "--quiet", defaultFile)
		fmt.Printf("Moved %s to %s and untracked it — commit the deletion when ready.\n", defaultFile, target)
	} else {
		fmt.Printf("Moved %s to %s.\n", defaultFile, target)
	}
	return true
}
```

`runInit` gains flag parsing at the top (before target resolution):

```go
	assumeYes := false
	var rest []string
	for _, a := range args {
		switch a {
		case "--yes", "-y":
			assumeYes = true
		default:
			rest = append(rest, a)
		}
	}
	args = rest
```

Then, for the default target only: resolve `root` (git toplevel of the working dir, else the working dir), call `migrateLegacyBoard(root, assumeYes)` before the exists-check, and with `assumeYes` skip the picker (use `defaultSections()`), and run `writeClaudeNote` + `writeReconcileHook` directly instead of `offerClaudeHook`. `offerClaudeHook`'s `stdinIsTerminal` gate stays for the interactive path.

Note: `git(root, "ls-files", "--error-unmatch", defaultFile)` returns `ok == true` when the file is tracked (exit 0). The existing `git` helper already gives that.

**README updates** (README.md): replace `SIDECAR.md` with `.sidecar/sidecar.md` in usage prose, document `sidecar diff` in the command table, and state the layout in one line: "Sidecar keeps its state in `.sidecar/` — the board (`sidecar.md`) and the last-turn snapshot (`previous.md`) — excluded from git via `.git/info/exclude`." Keep the Apple Developer voice already in the README; don't restructure it.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -run 'TestMigrateLegacyBoard|TestRunInitYes' ./...` then the full suite `go test ./...`
Expected: PASS, everything green. Smoke the migration end to end: build, `cd $(mktemp -d) && git init -q && printf '## 🧠 Needs action\n\n- x\n' > SIDECAR.md && /tmp/sidecar-dev init --yes && ls .sidecar/` — shows `sidecar.md`; `cat .claude/settings.json` shows the guarded `sidecar diff` hook.

- [ ] **Step 5: Commit**

```bash
git add init.go init_test.go main.go README.md
git commit -m "feat: init --yes, legacy SIDECAR.md migration, README for .sidecar/ layout

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Final verification (after Task 6)

- [ ] `go test ./...` — full suite green.
- [ ] `go vet ./...` — clean.
- [ ] Dogfood: in this repo, run the built binary's `init` (interactive), accept migration of the real SIDECAR.md, confirm the hook in `.claude/settings.json` is guarded, make a board edit, and check `sidecar diff` prints the semantic line plus reminder.
- [ ] Reconcile the board: move the #9 entry to ✅ Done once merged.
