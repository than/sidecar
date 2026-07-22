# sidecar

Sidecar is your AI's TODO list and scratchpad for its human companion: a
live-updating, scrollable markdown viewer for a narrow terminal pane. Built
to sit beside a Claude Code session and watch the `REVIEW.md` queue Claude
maintains during long working sessions.

```
sidecar [file.md]        # default: ./SIDECAR.md
sidecar init [file.md]   # scaffold the file, optionally keep it out of git
```

`sidecar init` writes a starter `SIDECAR.md` and, inside a git repo, offers
to keep it out of version control — via `.git/info/exclude` (uncommitted;
the ignore rule applies in every worktree) or `.gitignore` (committed) —
since a personal scratchpad usually shouldn't be tracked. It then offers to
add a note to `CLAUDE.md` (and optionally a `SessionStart` hook) so Claude
Code sessions keep the queue updated and know how to install/launch sidecar.

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

## Install

### Prebuilt binary (no Go needed)

Grab the archive for your OS/arch from the
[latest release](https://github.com/than/sidecar/releases/latest), extract,
and move `sidecar` onto your `PATH`. For example, on Apple Silicon:

```
curl -sSL https://github.com/than/sidecar/releases/latest/download/sidecar_$(uname -s)_$(uname -m).tar.gz | tar xz
mv sidecar ~/.local/bin/
```

(macOS `uname -m` reports `arm64`; Linux x86-64 reports `x86_64` — both
match the release archive names.)

### With Go

One command (needs Go 1.26+):

```
go install github.com/than/sidecar@latest
```

That installs `sidecar` into `$(go env GOPATH)/bin` (default `~/go/bin`) —
make sure it's on your `PATH`. To install straight into a dir that already
is, set `GOBIN`:

```
GOBIN=~/.local/bin go install github.com/than/sidecar@latest
```

### From source

```
git clone https://github.com/than/sidecar && cd sidecar
go build -o sidecar
ln -sf "$(pwd)/sidecar" ~/.local/bin/sidecar   # any dir on your PATH
```

## Test

```
go test ./...
```

`testdata/REVIEW.md` is a representative fixture: h1, emoji-marked h2
sections (🔴🟡🟢✅), bold, bullets, bare URLs, and a 300-line body for
scroll testing.

[glamour]: https://github.com/charmbracelet/glamour

## License

MIT — see [LICENSE](LICENSE).
