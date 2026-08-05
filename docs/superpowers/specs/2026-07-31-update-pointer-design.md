# Update pointer — mark what changed on reload

Date: 2026-07-31
Status: approved design, pending spec review

## Problem

When the watched file changes, the viewer re-renders the whole document and
flashes the status bar amber, but gives no cue to _where_ the change was. On a
long queue the human has to hunt for what moved. Sidecar should point at the
changed lines.

## Behavior

1. **Per-line change detection.** On each render, diff the previous baseline
   content against the current content at the current pane width, producing the
   set of rendered lines that are new/changed.
2. **Persistent&#x20;**`▸`**&#x20;marker (bullets only).** Every changed line that is a
   bullet has glamour's `• ` prefix replaced with a bright `▸ ` (identical
   width — no layout shift). Changed non-bullet lines (headings, prose) get no
   marker. The marker persists until the next content change.
3. **Subtle one-shot flash.** On a content change, all changed lines get a
   gentle background lightening for \~500 ms, then a timer clears it, leaving the
   `▸` behind. Single fade, no repeat/strobe. Toggleable with `--no-flash`
   (flash on by default). This is independent of the existing amber status-bar
   flash.

## Change detection (resize-proof)

The model keeps `prevBaseline string` — the raw content as of _before_ the most
recent content change. Markers are always computed as a line-level diff of
`render(prevBaseline, width)` vs `render(raw, width)`, recomputed on every
render. Because it re-diffs from raw at the current width, the marker set
survives a terminal resize / rewrap.

- On a **content change** (new raw differs from current `raw`): set
  `prevBaseline` to the *old* `raw` before replacing it. The diff then marks
  exactly what that change introduced, and persists (recomputed identically)
  until the next content change.
- On a **forced re-render with no content change** (resize): `prevBaseline` and
  `raw` are unchanged, so the same lines are marked, recomputed at the new
  width.
- **Initial load**: `prevBaseline` is empty; treat an empty baseline as "no
  markers" (don't mark the entire first render as changed).

### Line diff

A standard longest-common-subsequence over the two slices of rendered lines. A
line in the new render that is not part of the LCS with the old render is
"changed" (added or modified). This prevents an inserted line from cascading a
"changed" mark onto every identical line below it. Document sizes are hundreds
of lines, so an O(n·m) LCS table is fine.

Comparison is on the **visible text** of each line (ANSI stripped via the
existing `visibleWidth`/reflow helpers), so a pure restyle with identical text
isn't flagged; content is what matters.

## Rendering the markers

`reload()` renders markdown to a string as today, then post-processes:

1. Split into lines.
2. Compute the changed-line set (per above). Empty baseline → empty set.
3. For each line, if its index is in the changed set **and** the line is a
   bullet, replace the bullet with `▸ ` styled in `colorUpdated`. Bullet
   detection/replacement: the `•` glyph is emitted as a literal character in the
   rendered line (only surrounded by ANSI color codes, not encoded by them), so
   locate the first literal `• ` and replace that single occurrence with the
   styled `▸ `. A line counts as a bullet only when its first visible
   (ANSI-stripped) content is the `• ` prefix, so a `•` inside body text isn't
   matched. Indentation before the bullet is preserved, so nested bullets align.
4. If the flash is active, wrap every changed line (bullet or not) with a
   `colorFlashLineBg` background for its full rendered width.
5. Join and `SetContent`, preserving the scroll offset (as today).

The composition is a pure function of `(renderedLines, changedSet, flashActive)`
so it can be re-run cheaply when only the flash state changes.

## Flash lifecycle

- On a content change, set `lineFlash = true` and schedule a
  `lineFlashOffMsg` via `tea.Tick(~500 ms)`. Reuse the existing flash-timer
  pattern in `ui.go`.
- On `lineFlashOffMsg`, set `lineFlash = false` and re-`SetContent` from the
  cached rendered lines + changed set (scroll preserved) so the background
  clears but the `▸` stays.
- A resize while flashing recomputes normally and keeps `lineFlash` as-is.
- `--no-flash` sets a model field that suppresses step 4 and the timer entirely;
  the `▸` marker still works.

## Colors (new consts in `style.go`)

- `colorUpdated = "#5FE3A1"` — bright teal-green for the `▸` (tunable).
- `colorFlashLineBg = "#2A2A33"` — a subtle lightening just above the normal
  terminal background (tunable).

## Files

- `style.go` — the two color constants.
- `diff.go` (new) — `changedLines(oldLines, newLines []string) map[int]bool`
  (LCS), and `composeMarked(lines []string, changed map[int]bool, flash bool)
  string` (bullet `•`→`▸` swap + optional flash background).
- `ui.go` — add `prevBaseline`, `lineFlash`, `noFlash` fields; wire the diff and
  composition into `reload()`; add the `lineFlashOffMsg` + tick handling.
- `main.go` — parse `--no-flash`, thread into `newModel`.
- `diff_test.go` (new), `ui_test.go` (extend).

## Testing

- `changedLines`: identical inputs → empty set; a changed line → just that
  index; an **inserted** line → only the inserted index (not everything below);
  a deleted line → the surrounding context isn't spuriously marked.
- `composeMarked`: a changed bullet line has `• ` replaced by a styled `▸ `
  with indentation preserved; a changed non-bullet line is unchanged by the
  marker step; nested/indented bullets keep alignment.
- Flash off (`noFlash=true` or after `lineFlashOffMsg`): no background styling
  present; `▸` still present.
- `ui.go`: after a content-change reload the model reports changed lines and
  `lineFlash=true` with a scheduled command; `lineFlashOffMsg` clears the flash
  but a subsequent render still shows `▸`; initial load marks nothing; a resize
  (no content change) preserves the marker set.

## Out of scope (YAGNI)

- No per-word/intra-line highlighting — line granularity only.
- No marker for non-bullet changed lines (headings/prose) — bullets only, per
  the chosen design.
- No configurable colors beyond editing the `style.go` consts.
- No fade animation frames — a single flash-on then flash-off, nothing
  in between.
