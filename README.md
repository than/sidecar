# 🚗 Sidecar

Sidecar is a live, scrollable Markdown viewer for a narrow terminal pane. It’s designed to sit beside a Claude Code session and show the `SIDECAR.md` queue your agent keeps while it works — a to-do list and scratchpad your AI maintains for you.

```
sidecar [file.md]        # default: ./SIDECAR.md
sidecar init [file.md]   # create the file, and optionally keep it out of git
```

## Why two panes

In a long agent session, the status that matters — what’s done, what’s blocked, what shipped — scrolls out of view at the top of the transcript. Sidecar keeps it in place.

Run Sidecar beside your Claude Code session in a split terminal. Claude edits `SIDECAR.md` in one pane, and Sidecar renders it live in the pane next to it. You get a steady, current view of the work while the transcript moves on the other side.

| Claude Code | sidecar `SIDECAR.md` |
| --- | --- |
| edits `SIDECAR.md`,<br>transcript scrolls | 🧠 Needs action<br>🚧 In progress<br>🚘 Parked<br>✅ Done<br>📦 Shipped |

Any split-pane setup works — [Supacode], tmux, or your terminal’s built-in splits (Ghostty, iTerm2, WezTerm). To keep the queue current, use the `UserPromptSubmit` hook that `sidecar init` offers (option `[b]`). It reminds Claude to update the file each turn, so the view stays fresh. For details, see [Connect it to Claude Code](#connect-it-to-claude-code).

[Supacode]: https://supacode.sh

## Connect it to Claude Code

`sidecar init` creates a starter `SIDECAR.md`. Inside a git repository, it offers to keep the file out of version control — through `.git/info/exclude` (uncommitted; the rule applies in every worktree) or `.gitignore` (committed) — because a personal scratchpad usually shouldn’t be tracked.

It then offers to add a note to `CLAUDE.md`, and optionally a per-turn `UserPromptSubmit` reconcile hook, so your Claude Code sessions keep the queue up to date and know how to install and launch Sidecar. The hook merges into an existing `.claude/settings.json`, so running `sidecar init` again upgrades an earlier setup in place.

## What it does

- Renders Markdown with [glamour] and re-renders the moment the file changes. Sidecar watches the parent directory with fsnotify and a 100-millisecond debounce, so it handles atomic rename-swaps, deletes, and recreates — and waits quietly when the file doesn’t exist yet.
- Scrolls with `j` and `k`, the arrow keys, `PgUp` and `PgDn`, and `g` and `G` for top and bottom. Sidecar keeps your scroll position across reloads. It doesn’t capture the mouse, so your terminal’s text selection and clickable links keep working.
- Reloads on demand with `r`, and quits with `q`.
- Re-renders when you resize the terminal, at the pane width minus 2. It never renders wider than the pane.
- Shows a thin status bar: the filename, the time since the last update, and the scroll percentage.

## Rendering style

The glamour style is built into the binary (`style.go`) and tuned for a narrow pane on a dark background:

- Compact, with at most one blank line between blocks, no margins, and no trailing-space padding.
- Hex (truecolor) colors throughout, never 256-palette indexes, which Ghostty remaps.
- Teal, underlined links. Bare URLs stay on their own line, so Ghostty can detect them and make them clickable.
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
