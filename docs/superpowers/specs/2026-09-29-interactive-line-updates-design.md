# Interactive line updates

The viewer gains one write path: the human acts on a board item and sidecar writes that one line change back. The per-turn hook already diffs the board, so the agent reads the decision on its next turn.

## Intent

- The human ticks a checklist item, answers a yes/no/and/or/done prompt, or sends an item to ✅ Done without typing in chat.
- Keyboard first. Native text selection and clickable links stay the default; mouse clicks work only while mouse mode (`M`) is on.

## Board syntax

Plain markdown, nothing new to parse beyond two line prefixes.

- `- [ ]` / `- [x]` — a checklist item. `x` toggles it.
- `  Ask: yes | no | done` — an indented line under an item; options are separated by `|` and can be any words (`and`, `or`, …).
- `  Answer: no` — written directly under the `Ask:` line. A new answer replaces the old one, so an item carries at most one.

## Keys

`]` / `[` select the next or previous item. With one selected: `x` ticks; `1`–`9` answer by position; `y` / `n` / `d` answer `yes` / `no` / `done` when offered; `d` otherwise moves the item to ✅ Done; `esc` deselects. `M` toggles mouse mode: a click selects an item, a click on its box ticks it.

## Writes

- `editItem` re-reads the file, locates the item by section label and exact item text, applies a pure text transform, and swaps the file in by rename, keeping its mode.
- An edit by the agent elsewhere in the file survives. If the item itself changed since the viewer rendered it, nothing is written and the status bar says so.
- Only three mutations exist: toggle a checkbox, set an answer, move an item to Done. Free-form editing is out of scope.

## Agent side

- A task-list marker is state, not identity, so `normalizeItem` strips it: ticking reads as `edited`, not remove-plus-add.
- `sidecar diff` appends `— ticked`, `— unticked`, or `— answered "x"` to the edited line.
- The CLAUDE.md note teaches the `Ask:` / `Answer:` convention through `entryStyleRules`.

## Known limits

- The item cursor maps items to rendered lines by their column-0 marker (`•`, `□`, `✓`). A section whose rendered count differs from the parsed count, such as a collapsed section, is skipped rather than mis-selected.
- The rename swap leaves a small window in which a concurrent write by the agent to the same file is lost; the exact-text match narrows it to the same item.
