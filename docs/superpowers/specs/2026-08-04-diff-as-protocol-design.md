# Diff as protocol

Status: approved design, pending spec review
Issue: https://github.com/than/sidecar/issues/9
Date: 2026-08-04

## Problem

Each time the human sends a message, the reconcile hook repeats the same
static reminder. The agent learns that the board *may* have changed, so it
re-reads the whole file — every turn, whether anything moved or not. And
changes the human makes by hand never reach the agent at all.

Replace the static reminder with the actual changes since the last turn.
One mechanism, two payoffs: the agent syncs state in a few lines instead of
a full re-read, and the human's in-pane edits arrive as explicit events.
This is the transport that #8 (interactive moves in the TUI) builds on.

## Layout: `.sidecar/` is sidecar's project home

All project-local sidecar state lives in one directory, and the whole
directory is gitignored. The board is a local scratchpad, not shared repo
state.

```
.sidecar/
  sidecar.md   the board — replaces root-level SIDECAR.md
  prev.md      the board as of the last turn (the diff baseline)
```

- Bare `sidecar` opens `.sidecar/sidecar.md` when it exists.
- Future project-local state (config, #8) also lands here — no new dotfiles.
- Two things stay put: the hook (Claude Code reads `.claude/settings.json`)
  and this spec's changes to the CLAUDE.md note (agents find the board
  through it).
- Consequence, by design: a fresh clone starts with an empty board. History
  lives on the owner's machine only.

## New subcommand: `sidecar diff`

Compare `.sidecar/sidecar.md` with `.sidecar/prev.md`, print what changed,
then overwrite `prev.md` with the current board.

Behavior:

- **No changes** — print nothing, exit 0. The hook stays silent.
- **First run** (no `prev.md`) — save the snapshot silently.
- **No board** — print nothing, exit 0. Never an error in a hook context.
- **Changes** — print one line per changed item, then one closing line:
  the existing reconcile reminder, so the agent knows what file this is
  and what to do about it.

An optional path argument (`sidecar diff path/to/file.md`) overrides the
default board location; the snapshot then lives beside the usual one, keyed
by path. This keeps the command testable and usable outside the standard
layout.

### Semantic output

Items are bullets under a recognized section heading. Match items across
the two versions by their normalized text so a move reads as a move, not a
delete plus an add:

```
moved 🚧→✅: "Configurable sections TUI"
added 🧠: "reload the setup guide"
edited 🚘: "Picker: allow clearing an already-set emoji"
removed 🧠: "old idea"
```

- **moved** — same item, different section. The highest-signal event.
- **added / removed** — item appears in or disappears from one section.
- **edited** — same section, text changed enough to still match as the
  same item (prefix match on the normalized first line), but not equal.
- Multi-line items are identified by their first line; continuation lines
  changing counts as **edited**.
- Section set comes from the board's own headings via the existing parser
  (`sections.go`) — custom sections work without configuration.

### Fallback

If either version fails to parse into recognized sections, print a plain
unified diff with zero context lines (`diff -U0` shape, computed
internally — no shelling out) instead of semantic lines. The closing
reminder line still follows.

## Hook and init changes

`sidecar init`:

1. Creates `.sidecar/`, writes the starter board to `.sidecar/sidecar.md`.
2. Appends `.sidecar/` to `.git/info/exclude` — local and uncommitted, so
   the repo never learns sidecar exists. Automatic, no prompt; skipped
   when already ignored. (Replaces today's 3-way exclude prompt: the
   board's home is decided, so there's nothing left to ask.)
3. Writes the UserPromptSubmit hook as a guarded one-liner:
   run `sidecar diff` when sidecar is installed, otherwise echo the
   current static reminder. The hook degrades to today's behavior instead
   of erroring on machines without the binary.
4. **Migration** — when a root-level SIDECAR.md exists, offer to move it
   to `.sidecar/sidecar.md`, and `git rm --cached` it when tracked (some
   setups already exclude it locally, e.g. via `.git/info/exclude`).
   Re-running init upgrades a prior sidecar hook in place, as it does
   today.
5. Updates the CLAUDE.md note to point at `.sidecar/sidecar.md`.

The reconcile reminder text (`reconcileMessage`) survives as both the
hook's no-binary fallback and the closing line of a non-empty diff.

### Install-path fixes that ship with this

- **CLAUDE.md note upgrades in place.** Today `writeClaudeNote` skips
  when the `sidecar:review-queue` marker is present, so a changed path or
  section set never propagates. Replace the content between the markers
  instead. Migration depends on this.
- **Claude wiring becomes the default.** The hook is now the product, so
  Enter on the wiring prompt installs note + hook (today it defaults to
  no, with the hook a separate opt-in). Declining stays available.
- **`sidecar init --yes`.** Non-interactive full install — board, exclude,
  note, hook — with no prompts. Today every prompt is TTY-gated, so an
  agent running init gets a bare file with no wiring at all.
- **Explicit paths keep working.** Bare `sidecar` and `sidecar init`
  target `.sidecar/sidecar.md`; `sidecar <file>` and `sidecar init <file>`
  behave as today, with the diff snapshot keyed by path and no migration.

## Testing

- Unit tests for the differ: added, removed, moved, edited, multi-line
  items, custom sections, unparseable fallback, first run, no changes,
  missing board.
- Init tests: `.sidecar/` creation, info/exclude append (fresh, existing,
  already-present), guarded hook JSON, SIDECAR.md migration, CLAUDE.md
  note replacement between markers, `--yes` non-interactive install.
- One PTY e2e: bare `sidecar` opens `.sidecar/sidecar.md`.

## Out of scope

- Interactive moves in the TUI — #8, next project, on this wire.
- Per-section actions (#4) and multi-writer concerns (#5).
- Watching more than one board per project.
