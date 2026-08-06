# Collapsible sections in the viewer

Closes: https://github.com/than/sidecar/issues/6

## Problem

`✅ Done` and `📦 Shipped` grow unbounded and push the live sections
(`🧠`/`🚧`/`🚘`) off-screen. The viewer needs a way to collapse finished
sections while keeping the underlying markdown file untouched — this is a
rendering-only feature.

## Scope

Full feature in one pass: header item counts, and a working collapse/expand
toggle with a section cursor. No persistence across restarts — collapse
state resets to the default rule each launch. No new state file.

## Data model

`model` gains three fields:

- `board Board` — parsed via the existing `parseBoard` (board.go), refreshed
  on every reload.
- `collapsed map[string]bool` — keyed by `BoardSection.Label` (the heading
  text without `## `, e.g. `"✅ Done"`). Persists across reloads within a
  single run (in-memory only).
- `cursor int` — index into `board.Sections`, which section Tab/Shift+Tab is
  currently on.

## Rendering: rewrite raw markdown before glamour

A new `applyCollapse(raw string, board Board, collapsed map[string]bool)
string` step runs between reading the file and calling `renderMarkdown`:

1. For each section, append `(N)` to its heading line, where N is the
   section's item count excluding the `nothing yet` placeholder
   (`emptySectionPlaceholder` in sections.go). Shown for every section,
   including `(0)` — no special-casing for the empty case.
2. If the section is collapsed, its body bullets are omitted entirely from
   the rewritten raw text.
3. Everything else about the raw text passes through unchanged.

This keeps `renderMarkdown`, `changedLines`, and `composeMarked` untouched —
they operate on whatever (shorter) text comes out of `applyCollapse`, exactly
as they do today.

`parseBoard` returning `ok=false` (a file with no `## ` headings, or a
non-standard board) means `applyCollapse` is a no-op passthrough — the
collapse feature is inert for such files, matching the current fallback
behavior elsewhere in the app.

## Default collapse rule

The first time a section label is seen (first successful parse after
launch), seed `collapsed[label] = true` only for the exact labels
`"✅ Done"` and `"📦 Shipped"`. Every other label defaults to `false`. This
means custom section sets from `sidecar init` are not auto-collapsed unless
they happen to reuse those exact labels — no guessing at intent for
arbitrary section names.

Labels no longer present after a reload leave a harmless orphaned map entry;
newly appearing labels get the default seed.

## Cursor and keybinds

- `Tab` / `Shift+Tab` move the cursor to the next/previous section, wrapping
  at both ends.
- `Enter` / `Space` toggle `collapsed[section.Label]` for the section at the
  cursor.
- Moving the cursor scrolls the viewport to keep that section's header line
  visible (clamps `YOffset` with a small margin, same idea as the existing
  `GotoTop`/`GotoBottom` handling for `g`/`G`).
- These keys are new — they don't collide with the viewport's own
  scroll-key handling (arrows, `j`/`k`, page up/down all still scroll as
  today).

### Locating header lines after render

Glamour renders every `## ` heading with the `▍ ` prefix (H2 style in
style.go). After `renderMarkdown` produces the line slice, a helper walks it
once and collects the index of every line whose ANSI-stripped, left-trimmed
text starts with `▍ ` — in document order. That index list lines up 1:1
with `board.Sections`, since every board section is an H2 and appears in
the same order in both. `sectionHeaderLines[i]` maps section `i` to its
rendered line index.

### Highlighting the cursor's header

Reuses the existing `applyLineBg` mechanism (diff.go) — already used for the
post-reload line flash — with a new `colorCursorBg` constant (style.go).
Applied unconditionally to `sectionHeaderLines[cursor]` on every render,
not time-limited like the flash. This avoids inventing a second
line-tinting code path for what is visually the same kind of effect.

## Edge cases

- **No headings**: `applyCollapse` is a no-op; Tab/Enter have nothing to
  act on and are ignored.
- **Reload while collapsed**: `collapsed` map is untouched by `reload()`;
  only first-ever-seen labels get the default seed.
- **Section removed/renamed**: stale map entries are harmless; new labels
  get fresh default state.
- **Cursor out of range** (section count shrank on reload): clamp to
  `len(board.Sections)-1` (or 0 if no sections).
- **Resize**: already forces `m.reload(true)` at the new wrap width; cursor
  and collapse state are untouched, just re-rendered at the new width.

## Testing

- `applyCollapse` unit tests: count formatting (including the `(0)` case),
  placeholder excluded from the count, collapsed section body stripped,
  expanded section body intact, default-collapse seeding applies only to
  `"✅ Done"`/`"📦 Shipped"`.
- Header-line-index mapping: a rendered doc with N sections maps to N
  indices in document order.
- `ui_test.go` additions: Tab/Shift+Tab wraparound at both ends; Enter/Space
  toggling changes rendered output (collapsed section's items disappear,
  count stays); collapse state survives a simulated reload.
