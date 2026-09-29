# Interactive line updates

The viewer is the human's channel back to the agent. The agent owns the board and every move on it; the human only says "I did this" and answers questions, and the hook cycle hands both to the agent mid-turn.

## Intent

- An agent leaves the result of a turn as an entry. The human, who may be in another tab, says what they did in between, in their own words.
- The agent asks a narrative question, not a yes/no.
- Always clickable: mouse capture is on by default (`--no-mouse` and `M` release it). Shift-drag (Option in iTerm2) still selects text.
- No emoji, no buttons. The characters already on the board are the controls.

## What the human sees and does

- **The bullet is the control.** Click within the first four columns of an item's first line and `•` turns into `✓`; the file gets `- [x]`. Click again and it is a plain bullet. `space` does the same on the selected item.
- **`? question`** — an `Ask:` line is drawn as an indented `? …` block in one accent color, wrapped with a hanging indent. Click any line of it, or press `enter` on the item, and a reply line opens under the item; Enter writes it, esc abandons it. The reply shows as `↳ …`. The reply line is pre-filled with the current reply so it can be edited, and shows `type your reply… ⏎ send · esc cancel` when empty.
- **Links** open on a click. Capture stops the terminal handling OSC 8 links, so the viewer finds the link under the pointer in the rendered line and opens it (web and mail schemes only; never `file:` or an app's scheme). The full address shows in the status bar on hover.
- Whatever is clickable — bullet, question, or link — turns solid under the pointer; nothing else changes on hover.
- One blank line separates items.
- The viewer never moves an item between sections. Filing stays with the agent.

## Board syntax

Plain markdown; the file never contains a control.

- `  Ask: what did you change?` — the agent's question.
- `  Answer: …` — the human's reply. It is written under `Ask:` when there is one and last otherwise, so an unprompted reply works on any item.
- `- [x] …` — ticked. `[ ]` and plain bullets both tick to `[x]`; unticking returns to a plain bullet.

## Delivery

- `sidecar diff` (UserPromptSubmit) is unchanged and reports everything since the last prompt.
- `sidecar diff --mid-turn` (PostToolUse) prints hook JSON only when the human replied or ticked, and stays silent for anything else, including the agent's own edits. It advances the snapshot only when it reported, so a reply arrives once.
- `sidecar init` installs both hooks; the mid-turn one carries the same sentinel, so a re-run replaces it.
- The diff line reads `edited 🧠: "Title" — replied "…"`; a tick reads `— ticked`, an untick `— unticked`.

## Writes and undo

- `editItem` re-reads the file, finds the item by section label and exact text, applies a pure text transform, and swaps the file in by rename, keeping its mode. An agent edit elsewhere survives; if the item itself changed since it was rendered, nothing is written and the status bar says so.
- Every write is remembered with the file before and after. `u`, or a click on the status message, restores the earlier file — but only while the file still reads exactly as that write left it, so undo never overwrites the agent.
- Every click reports its outcome in the status bar, so none looks ignored.

## Keyboard

`j` / `k` move between items (they scroll when the board has none; arrows and the wheel always scroll), `space` ticks, `enter` replies, `}` / `{` jump between unanswered questions, `u` undoes, `esc` deselects, `tab` moves between sections. The status bar counts open questions.

## Known limits

- Items are mapped to rendered lines by their column-0 marker (`•`, `□`, `✓`); a section whose rendered count differs from the parsed count (collapsed) is skipped rather than mis-selected.
- When `--mid-turn` reports, the snapshot advances whole, so an agent edit made in the same window is not repeated in the next prompt's diff.
- A click is held while a reply is being typed, so it cannot act on a line the typing row has shifted.
