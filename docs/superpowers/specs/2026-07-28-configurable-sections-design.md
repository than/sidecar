# Configurable sections + interactive picker (issue #3)

Date: 2026-07-28
Status: approved design, pending spec review

## Problem

`sidecar init` writes a fixed set of five sections (🧠 Needs action / 🚧 In
progress / 🚘 Parked / ✅ Done / 📦 Shipped) into three places. Users can't
choose their own section names, emoji, or order without hand-editing all three
afterward.

## Key constraint (scopes the whole feature)

The viewer renders markdown **generically** — it never parses section names
(`ui.go` → `renderMarkdown`). Sections are purely a convention. So this feature
is **init-time only**: it changes what `init` writes, nothing about rendering,
and nothing persists for the viewer to read back.

## Data model

``` go
type Section struct {
    Emoji string // literal emoji char(s), may be empty (text-only section)
    Name  string // e.g. "Needs action"
    Hint  string // optional one-line meaning, e.g. "surfaced for the human"
}
```

An ordered `[]Section`. The default value is today's five, each with its
current hint. `Section.Header()` returns `## 🧠 Needs action` (or `## Todo`
when `Emoji` is empty).

## Three consumers, refactored to take `[]Section`

Currently hardcoded strings; become generated from the chosen sections:

1. **Starter template** (`init.go` `starterTemplate` const → `func`). Headers
   in order, each followed by `- nothing yet`. The leading `<!--` comment lists
   each section with its hint (dynamic — replaces today's hardcoded
   `🚘 Parked = deferred` lines).
2. **CLAUDE.md note** (`claudeNote`). Lists the chosen headers and embeds each
   section's hint so Claude knows what each means.
3. **Reconcile hook message** (`reconcileMessage`). Lists the chosen headers in
   order (the `Sections: … / … / …` tail).

Sections with an empty `Hint` are simply listed without a `= meaning` clause.

## The picker

A **separate short-lived bubbletea program** run during `init` (and the
launch-time create prompt in `offerCreate`), positioned **after** file creation
and **before** the git-exclude / CLAUDE.md prompts.

- **Non-interactive stdin** (piped / not a terminal) → skip the picker, use the
  default five. Keeps scripts and tests unblocked. Guarded by the existing
  `stdinIsTerminal()`.
- **Styling**: reuses the lipgloss palette in `style.go` so it matches the
  viewer. The picker is styled; **what it writes to disk is plain markdown**.

### Layout

```
  Customize your sidecar sections
  jk move · space toggle · J/K reorder · e edit · a add · d delete · ⏎ done

  ▸ [x] 🧠 Needs action    — surfaced for the human to act on
    [x] 🚧 In progress     — actively being worked
    [x] 🚘 Parked          — deferred, not dropped
    [x] ✅ Done            — merged, not yet released
    [x] 📦 Shipped         — released
```

### Keys

Primary keys stay in the letter cluster; arrow keys work as silent aliases but
aren't advertised.

- `j` / `k` — move cursor (`↓` / `↑` also work)
- `space` — toggle include (`[x]` / `[ ]`)
- `J` / `K` — move the cursor row down / up in order
- `e` — edit cursor row (see edit mode)
- `a` — add a blank section at the cursor, drop straight into edit mode
- `d` — delete cursor row
- `⏎` — accept: write only included rows, in displayed order
- `esc` / `q` — cancel: fall back to the default five

### Edit mode

A single `bubbles/textinput` line at the bottom, one field at a time:
emoji → name → hint. Each pre-filled with the current value; Enter keeps it and
advances. Rules:

- Empty **name** on accept → the row is dropped (covers add-then-cancel).
- **Emoji** is optional → text-only sections like `## Todo` are allowed.
- **Hint** is optional → section listed without a meaning clause.

## Error handling / edge cases

- Piped stdin → default five, no picker (as above).
- User cancels picker (`esc`/`q`) → default five.
- User toggles everything off / deletes all → treat as cancel, fall back to
  default five (never write a file with zero sections).
- bubbletea program error → log to stderr, fall back to default five; `init`
  still scaffolds the file.
- Duplicate section names → allowed (user's choice); no dedup.

## Testing

- `Section.Header()`: emoji present, emoji empty.
- Template generation: default set produces the five expected headers in order,
  each with `- nothing yet`, and a `<!--` comment listing every section's hint.
  (Not byte-identical to today's freeform comment — the comment is now generated
  from per-section hints, so the default template's comment changes shape.)
  Custom set produces its expected headers + comment.
- `claudeNote` / `reconcileMessage`: generated from a custom `[]Section`,
  including hint-present and hint-empty rows.
- Empty / all-deselected section list → falls back to default five.
- Picker model unit tests (bubbletea `Update`): toggle, reorder (J/K bounds),
  add, delete, edit field advance, cancel. Drive via synthetic `tea.KeyMsg`
  like the existing `ui_test.go`.
- Non-interactive path: `init` with piped stdin still writes the default
  template (existing behavior preserved).

## Out of scope (YAGNI)

- No config file / persistence; the viewer never reads sections back.
- No re-running the picker against an existing file (init leaves existing files
  untouched today; unchanged).
- No per-section runtime rendering behavior.

## Follow-up (tracked separately, not this spec)

- README two-pane ASCII diagram: emoji are double-width, pushing the right box
  border out of alignment. Cosmetic README fix.
