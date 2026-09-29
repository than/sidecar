# Interactive line updates

The viewer gains one write path so the human can answer the agent from the board, and the hook cycle hands the answer to the agent mid-turn.

## Intent

- An agent leaves the result of a turn as an entry. The human, who may be in another tab, says what they did in between, in their own words.
- The agent asks a narrative question, not a yes/no.
- Always clickable: mouse capture is on by default (`--no-mouse` and `M` release it). Shift-drag (Option in iTerm2) still selects text.

## What the human sees

Under every open item — everything outside ✅ Done and 📦 Shipped — two buttons:

    [ ✅ Done ]  [ 💬 Reply ]

- Buttons rest as quiet grey blocks and turn solid (green Done, blue the rest) under the pointer, so a screen of items stays calm. They are drawn after rendering, on their own line below each item.
- **Reply** opens a free-text line under the item, where the button row was, pre-filled with the current reply. Enter writes `Answer: <text>` under the item, replacing an earlier one; esc abandons it. While typing, every key is text.
- **Done** moves the item to ✅ Done. When the human has said nothing else it also writes `Answer: ✅ Done`, so the hook can tell a human finished it.
- An `Ask:` line with no `|` is the question. It shows as `💬 <question>` above the buttons. An `Ask: a | b` line is a choice and draws one button per option; the recorded choice is ticked.

## Board syntax

Plain markdown; the file never contains a button.

- `  Ask: what did you change?` — the agent's question.
- `  Ask: ✅ Go | ❌ Hold` — optional choices.
- `  Answer: …` — the human's reply. It is written under `Ask:` when there is one and last otherwise, so a reply works on an item that asked nothing.
- `- [ ]` / `- [x]` — a checklist item; the box ticks on a click in the first two columns or `x`.

## Delivery

- `sidecar diff` (UserPromptSubmit) is unchanged and reports everything since the last prompt.
- `sidecar diff --mid-turn` (PostToolUse) prints hook JSON only when the human replied or ticked, and stays silent for anything else, including the agent's own edits. It advances the snapshot only when it reported, so a reply arrives once.
- `sidecar init` installs both hooks; the mid-turn one carries the same sentinel, so a re-run replaces it.
- The diff line reads `edited 🧠: "Title" — replied "…"`; a tick reads `— ticked`.

## Writes

`editItem` re-reads the file, finds the item by section label and exact text, applies a pure text transform, and swaps the file in by rename, keeping its mode. An agent edit elsewhere survives; if the item itself changed since it was rendered, nothing is written and the status bar says so.

## Undo

Every write made from the viewer is remembered with the file as it stood before and after. `u`, or a click on the status message, restores the earlier file — but only while the file still reads exactly as that write left it, so undo can never overwrite what the agent wrote since. Every click also says what it did in the status bar, so none looks ignored.

## Keyboard

`]` / `[` select an item, `a` replies, `d` sends it to Done, `x` ticks, `1`–`9` and `y` / `n` / `d` pick a choice, `}` / `{` jump between unanswered questions, `esc` deselects. The status bar counts open questions.

## Known limits

- Rows are found by their column-0 marker; a section whose rendered count differs from the parsed count (collapsed) is skipped rather than mis-selected.
- When `--mid-turn` reports, the snapshot advances whole, so an agent edit made in the same window is not repeated in the next prompt's diff.
- Clicking a link: capture may stop the terminal's own link handling; the viewer does not open links itself yet.
