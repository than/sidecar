# sidecar

Sidecar is your AI's TODO list and scratchpad for its human companion: a
live-updating, scrollable markdown viewer for a narrow terminal pane. Built
to sit beside a Claude Code session and watch the `REVIEW.md` queue Claude
maintains during long working sessions.

```
sidecar path/to/FILE.md
```

## What it does

- Renders markdown with [glamour] and re-renders the moment the file
  changes (fsnotify on the parent directory, 100 ms debounce — survives
  atomic rename-swaps, deletes, and recreates; waits politely if the file
  doesn't exist yet).
- Scrolls: mouse wheel, `j`/`k`, arrows, `PgUp`/`PgDn`, `g`/`G` for
  top/bottom. Scroll position is preserved across reloads.
- `r` forces a reload, `q` quits.
- Re-renders on terminal resize at pane width − 2 — it never renders wider
  than the pane.
- Thin status bar: filename · "updated 12s ago" · scroll %.

## Rendering style

The glamour style is embedded in the binary (`style.go`), tuned for a
narrow pane on a dark background:

- Compact: at most one blank line between blocks, no margins, no
  trailing-space padding.
- All colors are **hex** (truecolor), never 256-palette indexes — Ghostty
  remaps palette indexes.
- Links are teal (leaning blue-cyan) and underlined; bare URLs stay intact
  on their own line so Ghostty's link detection makes them clickable.
- H1 is a black-on-lavender badge; H2 is muted amber with a `▍ ` prefix;
  bold stays default-foreground bold.

To tune colors, edit the `color…` constants at the top of `style.go`
(links are `colorLink`) and rebuild.

## Build & install

```
go build -o sidecar
ln -sf "$(pwd)/sidecar" ~/.local/bin/sidecar
```

`~/.local/bin` is on PATH; `/usr/local/bin` works too but needs sudo.

## Test

```
go test ./...
```

`testdata/REVIEW.md` is a representative fixture: h1, emoji-marked h2
sections (🔴🟡🟢✅), bold, bullets, bare URLs, and a 300-line body for
scroll testing.

[glamour]: https://github.com/charmbracelet/glamour
