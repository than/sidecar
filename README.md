# 🚗 Sidecar

Sidecar is a live, scrollable Markdown viewer for a narrow terminal pane. It’s designed to sit beside a Claude Code session and show the `.sidecar/sidecar.md` queue your agent keeps while it works — a to-do list and scratchpad your AI maintains for you.

```
sidecar [file.md]        # default: .sidecar/sidecar.md
sidecar init [file.md]   # create the board, and optionally keep it out of git
sidecar diff [file.md]   # print board changes since the last run
sidecar --no-flash [file]  disable the subtle change-flash (▸ still shows)
```

Sidecar keeps its state in `.sidecar/` — the board (`sidecar.md`) and the last-turn snapshot (`previous.md`) — excluded from git via `.git/info/exclude`.

## Why two panes

In a long agent session, the status that matters — what’s done, what’s blocked, what shipped — scrolls out of view at the top of the transcript. Sidecar keeps it in place.

Run Sidecar beside your Claude Code session in a split terminal. Claude edits `.sidecar/sidecar.md` in one pane, and Sidecar renders it live in the pane next to it. You get a steady, current view of the work while the transcript moves on the other side.

| Claude Code | sidecar `.sidecar/sidecar.md` |
| --- | --- |
| edits `.sidecar/sidecar.md`,<br>transcript scrolls | 🧠 Needs you<br>🤖 Agent queue<br>🚧 In progress<br>🚘 Parked<br>✅ Done<br>📦 Shipped |

Any split-pane setup works — [Supacode], tmux, or your terminal’s built-in splits (Ghostty, iTerm2, WezTerm). To keep the queue current, use the `UserPromptSubmit` hook that `sidecar init` installs by default. It reminds Claude to update the file each turn, so the view stays fresh. For details, see [Connect it to Claude Code](#connect-it-to-claude-code).

[Supacode]: https://supacode.sh

## Connect it to Claude Code

`sidecar init` creates a starter `.sidecar/sidecar.md` and wires it into Claude Code — no questions asked. Every recommended default applies: its home is excluded from git automatically (appended to `.git/info/exclude`, uncommitted, applying in every worktree, because a personal scratchpad usually shouldn’t be tracked), a note goes into `CLAUDE.md`, and a per-turn `UserPromptSubmit` reconcile hook merges into `.claude/settings.json` so Claude Code keeps the queue current and knows how to install and launch Sidecar. Running `sidecar init` again upgrades an earlier setup in place. If a root-level `SIDECAR.md` from an older Sidecar exists, it’s migrated into `.sidecar/sidecar.md` for you, silently.

Two flags opt out of a default: `--no-claude` skips the `CLAUDE.md` note and reconcile hook; `--keep-board` leaves a legacy root `SIDECAR.md` where it is and points `init` at it directly, instead of migrating to `.sidecar/` (a no-op when `.sidecar/sidecar.md` already exists). `--yes`/`-y` skip the section picker described next, and keep `init` non-interactive end to end: from a terminal, a bare `sidecar init` opens the viewer on the board it just created, while `--yes` returns to the shell for scripts and muscle memory that need it to. Creating a brand-new board from an interactive terminal still opens a short picker for choosing its sections; pass a custom path (`sidecar init notes.md`) to place the board somewhere other than `.sidecar/sidecar.md`, which still gets the same automatic exclude, note, and hook.

Boards are per-directory. `sidecar init` refuses a board that is a symlink — the file itself, or the `.sidecar/` home it sits in — and writes nothing at all: no board, no `CLAUDE.md` note, no reconcile hook, so a linked board can't be re-inited to pick up a newer note until the link is gone. A link points two checkouts at one queue, and a linked `.sidecar/` shares one `previous.md` besides, so the per-turn `sidecar diff` reports another session's changes as this one's. Reading is unchanged: the viewer still opens a symlinked board that already exists.

## What it does

- Renders Markdown with [glamour] and re-renders the moment the file changes. Sidecar watches the parent directory with fsnotify and a 100-millisecond debounce, so it handles atomic rename-swaps, deletes, and recreates — and waits quietly when the file doesn’t exist yet.
- Scrolls with `j` and `k`, the arrow keys, `PgUp` and `PgDn`, and `g` and `G` for top and bottom. Sidecar keeps your scroll position across reloads. It doesn’t capture the mouse, so your terminal’s text selection and clickable links keep working.
- `Tab` and `Shift+Tab` move a cursor between sections; `Enter` or `Space` collapses or expands the section under it. `✅ Done` and `📦 Shipped` start collapsed — every heading shows its item count, e.g. `✅ Done (12)`.
- Reloads on demand with `r`, and quits with `q`.
- Re-renders when you resize the terminal, at the pane width minus 2. It never renders wider than the pane.
- Points at what changed: on reload, a changed bullet's `•` becomes a bright
  `▸` (until the next change), and changed lines get a brief, subtle background
  flash. Disable the flash with `--no-flash`.
- Shows a thin status bar: the filename, the time since the last update, and the scroll percentage.

## Rendering style

The glamour style is built into the binary (`style.go`) and tuned for a narrow pane on a dark background:

- Compact, with at most one blank line between blocks, no margins, and no trailing-space padding.
- Hex (truecolor) colors throughout, never 256-palette indexes, which Ghostty remaps.
- Teal, underlined links. Bare URLs render as real OSC 8 hyperlinks — clickable in any terminal that supports them, Ghostty included — with the visible text shortened to fit the pane when the URL itself is longer, though the link always opens the full, untruncated address. A URL alone on its line (the board convention) shows in full; one inside a sentence drops the `https://` and takes a reserved slice of the line, so it is never broken across the wrap. A markdown link renders as its label alone — `[the PR](https://…)` shows `the PR`, clickable, with the address hidden — which is usually the shortest way to reference something in an entry. Either way, in a terminal without OSC 8 support the visible text is just plain text — and note that `http://` and `https://` look identical inline, since only the link target keeps the scheme.
- An H1 badge in black on lavender, H2 headings in muted amber with a `▍ ` prefix, and bold text in the default foreground.

To adjust the colors, edit the `color…` constants at the top of `style.go` — links use `colorLink` — and rebuild.

## Install

### Prebuilt binary (no Go required)

Download the archive for your operating system and architecture from the [latest release](https://github.com/than/sidecar/releases/latest), extract it, and move `sidecar` onto your `PATH`. For example, on Apple silicon:

```
curl -sSL https://github.com/than/sidecar/releases/latest/download/sidecar_$(uname -s)_$(uname -m).tar.gz | tar xz
mv sidecar ~/.local/bin/
```

On macOS, `uname -m` reports `arm64`. On Linux x86-64, it reports `x86_64`. Both match the release archive names.

### With Go

Install with one command (Go 1.26 or later):

```
go install github.com/than/sidecar@latest
```

This installs `sidecar` into `$(go env GOPATH)/bin` — `~/go/bin` by default. Make sure that directory is on your `PATH`. To install into a directory that already is, set `GOBIN`:

```
GOBIN=~/.local/bin go install github.com/than/sidecar@latest
```

### From source

```
git clone https://github.com/than/sidecar && cd sidecar
go build -o sidecar
ln -sf "$(pwd)/sidecar" ~/.local/bin/sidecar   # any directory on your PATH
```

## Test

```
go test ./...
```

`testdata/REVIEW.md` is a representative fixture: an H1, emoji-marked H2 sections (🔴🟡🟢✅), bold text, bullets, bare URLs, and a 300-line body for testing scroll.

[glamour]: https://github.com/charmbracelet/glamour

## License

MIT. See [LICENSE](LICENSE).
