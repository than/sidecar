# Update Pointer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Mark the lines that changed on each reload — swap the bullet `• → ▸` (bright) on changed bullet lines and persist it until the next change, plus a subtle ~500 ms one-shot background flash on all changed lines.

**Architecture:** A new `diff.go` holds a line-level LCS (`changedLines`) and the display composition (`composeMarked` + ANSI helpers). `ui.go` keeps a `prevBaseline` (content before the last change) and re-diffs `render(prevBaseline)` vs `render(raw)` on every render, so markers survive resize. The flash is a timer (`lineFlashOffMsg`) mirroring the existing status-bar flash. A `--no-flash` flag disables the flash; the `▸` marker always works.

**Tech Stack:** Go, glamour (already renders), lipgloss (already used), `muesli/reflow` (already used by `visibleWidth`). No new dependencies.

## Global Constraints

- Module `github.com/than/sidecar`, package `main`, flat repo root.
- No new dependencies.
- The `▸`/flash composition operates on already-rendered ANSI lines. The `•`
  glyph is emitted literally (e.g. `\x1b[38;2;208;208;208m• \x1b[0m`), so swap
  the first literal `• `. Background tinting must be SGR-aware: prepend the bg
  code and re-apply it after every `\x1b[0m` reset, because a reset clears the
  background mid-line.
- Line comparison for the diff is on ANSI-stripped visible text, so a pure
  restyle isn't flagged.
- Never change the viewer's existing guarantees: scroll preservation across
  reloads, pane-width cap, the status-bar amber flash. `renderMarkdown`,
  `watcher.go`, and the parent-dir watch are untouched.
- Initial load (empty baseline) marks nothing.
- gofmt-clean, `go vet ./...` clean.

## Colors (added in Task 2, `style.go`)

- `colorUpdated = "#5FE3A1"` — bright teal-green `▸`.
- `colorFlashLineBg = "#2A2A33"` — subtle bg lightening.

---

### Task 1: Line diff + stripANSI

**Files:**
- Create: `diff.go`
- Modify: `render_test.go` — remove its local `stripANSI` (moved to `diff.go`)
- Test: `diff_test.go`

**Interfaces:**
- Produces:
  - `func stripANSI(s string) string` (moved to production)
  - `func changedLines(oldLines, newLines []string) map[int]bool` — indices into
    `newLines` that are not part of the LCS with `oldLines` (added/modified),
    comparing on ANSI-stripped text.

- [ ] **Step 1: Write the failing test**

```go
// diff_test.go
package main

import "testing"

func idx(m map[int]bool) []int {
	out := []int{}
	for i := 0; i < 100; i++ {
		if m[i] {
			out = append(out, i)
		}
	}
	return out
}

func eq(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestChangedLinesIdentical(t *testing.T) {
	l := []string{"a", "b", "c"}
	if got := idx(changedLines(l, l)); len(got) != 0 {
		t.Errorf("identical → %v, want none", got)
	}
}

func TestChangedLinesOneModified(t *testing.T) {
	old := []string{"a", "b", "c"}
	nw := []string{"a", "B", "c"}
	if got := idx(changedLines(old, nw)); !eq(got, []int{1}) {
		t.Errorf("modified → %v, want [1]", got)
	}
}

func TestChangedLinesInsertionNoCascade(t *testing.T) {
	// Inserting one line must mark ONLY the new line, not everything below.
	old := []string{"a", "b", "c"}
	nw := []string{"a", "NEW", "b", "c"}
	if got := idx(changedLines(old, nw)); !eq(got, []int{1}) {
		t.Errorf("insertion → %v, want [1] (no cascade)", got)
	}
}

func TestChangedLinesIgnoresANSI(t *testing.T) {
	old := []string{"\x1b[31mhello\x1b[0m"}
	nw := []string{"\x1b[32mhello\x1b[0m"} // same text, different color
	if got := idx(changedLines(old, nw)); len(got) != 0 {
		t.Errorf("restyle-only → %v, want none", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestChangedLines -v`
Expected: FAIL — `undefined: changedLines`

- [ ] **Step 3: Write minimal implementation**

```go
// diff.go
package main

import (
	"regexp"
	"strings"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// stripANSI removes SGR escape sequences, leaving the visible text.
func stripANSI(s string) string {
	return ansiRE.ReplaceAllString(s, "")
}

// changedLines returns the indices into newLines that are new or modified
// relative to oldLines — the lines not covered by the longest common
// subsequence of the two, compared on ANSI-stripped visible text. An inserted
// line marks only itself, not the identical lines shifted below it.
func changedLines(oldLines, newLines []string) map[int]bool {
	o := make([]string, len(oldLines))
	for i, l := range oldLines {
		o[i] = stripANSI(l)
	}
	n := make([]string, len(newLines))
	for i, l := range newLines {
		n[i] = stripANSI(l)
	}

	// LCS length table.
	lcs := make([][]int, len(o)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(n)+1)
	}
	for i := len(o) - 1; i >= 0; i-- {
		for j := len(n) - 1; j >= 0; j-- {
			if o[i] == n[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	// Walk the table; lines in n that aren't part of the common subsequence
	// are the changed ones.
	changed := map[int]bool{}
	i, j := 0, 0
	for j < len(n) {
		if i < len(o) && o[i] == n[j] {
			i++
			j++
		} else if i < len(o) && lcs[i+1][j] >= lcs[i][j+1] {
			i++ // a line from old was removed
		} else {
			changed[j] = true // n[j] is new/modified
			j++
		}
	}
	return changed
}
```

Then remove the duplicate `stripANSI` from `render_test.go` (delete its `func stripANSI(...)` block; the tests there now use the production one).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... -run 'TestChangedLines|TestScroll|TestReloadFlash' -v`
Expected: PASS (diff tests pass; existing render/ui tests still compile with the moved `stripANSI`)

- [ ] **Step 5: Commit**

```bash
git add diff.go diff_test.go render_test.go
git commit -m "feat: line-level LCS diff of rendered output (update pointer)"
```

---

### Task 2: Marker + flash composition

**Files:**
- Modify: `style.go` — add the two color constants
- Modify: `diff.go` — add `composeMarked`, `applyLineBg`, `isBulletLine`, `swapBullet`, `hexToRGB`
- Test: `diff_test.go`

**Interfaces:**
- Consumes: `changedLines` (Task 1), `visibleWidth` (`style.go`), `colorUpdated`/`colorFlashLineBg`.
- Produces:
  - `func composeMarked(lines []string, changed map[int]bool, flash bool, width int) string`

- [ ] **Step 1: Write the failing test**

```go
// diff_test.go (append)
import "strings" // add to the import block

func TestComposeMarkedBulletSwap(t *testing.T) {
	// A changed bullet line: the literal "• " becomes a styled "▸ ".
	lines := []string{"\x1b[38;2;208;208;208m• \x1b[0malpha"}
	out := composeMarked(lines, map[int]bool{0: true}, false, 40)
	if strings.Contains(stripANSI(out), "• ") {
		t.Errorf("bullet not replaced:\n%q", out)
	}
	if !strings.Contains(stripANSI(out), "▸ alpha") {
		t.Errorf("expected ▸ alpha, got:\n%q", stripANSI(out))
	}
}

func TestComposeMarkedUnchangedLineUntouched(t *testing.T) {
	lines := []string{"\x1b[38;2;208;208;208m• \x1b[0malpha"}
	out := composeMarked(lines, map[int]bool{}, false, 40) // nothing changed
	if out != lines[0] {
		t.Errorf("unchanged line altered:\n%q", out)
	}
}

func TestComposeMarkedNonBulletNoMarker(t *testing.T) {
	// A changed non-bullet line gets no ▸ (bullets only), and without flash
	// its text is unchanged.
	lines := []string{"\x1b[38;2;209;154;102m▍ Heading\x1b[0m"}
	out := composeMarked(lines, map[int]bool{0: true}, false, 40)
	if strings.Contains(out, "▸") {
		t.Errorf("non-bullet line should not get ▸:\n%q", out)
	}
}

func TestComposeMarkedFlashAddsBackground(t *testing.T) {
	lines := []string{"\x1b[38;2;208;208;208m• \x1b[0malpha"}
	out := composeMarked(lines, map[int]bool{0: true}, true, 40)
	if !strings.Contains(out, "\x1b[48;2;") {
		t.Errorf("flash should inject a background SGR:\n%q", out)
	}
	// The ▸ still shows through the flash.
	if !strings.Contains(stripANSI(out), "▸ alpha") {
		t.Errorf("▸ missing under flash:\n%q", stripANSI(out))
	}
}

func TestComposeMarkedNoFlashNoBackground(t *testing.T) {
	lines := []string{"\x1b[38;2;208;208;208m• \x1b[0malpha"}
	out := composeMarked(lines, map[int]bool{0: true}, false, 40)
	if strings.Contains(out, "\x1b[48;2;") {
		t.Errorf("no flash should not inject a background:\n%q", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestComposeMarked -v`
Expected: FAIL — `undefined: composeMarked`

- [ ] **Step 3: Write minimal implementation**

Add to `style.go` (near the other `color…` consts):

```go
	colorUpdated     = "#5FE3A1" // bright ▸ marking a changed line
	colorFlashLineBg = "#2A2A33" // subtle bg lightening on a just-changed line
```

Add to `diff.go`:

```go
import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// composeMarked renders the display string: changed bullet lines get their
// "• " swapped for a bright "▸ ", and (when flash is true) every changed line
// gets a subtle background tint. Order matters — the bullet is swapped first so
// applyLineBg re-establishes the background after the reset the swap introduces.
func composeMarked(lines []string, changed map[int]bool, flash bool, width int) string {
	updatedMark := lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorUpdated)).Bold(true).Render("▸ ")

	out := make([]string, len(lines))
	for i, ln := range lines {
		if changed[i] {
			if isBulletLine(ln) {
				ln = strings.Replace(ln, "• ", updatedMark, 1)
			}
			if flash {
				ln = applyLineBg(ln, colorFlashLineBg, width)
			}
		}
		out[i] = ln
	}
	return strings.Join(out, "\n")
}

// isBulletLine reports whether the line's first visible content is glamour's
// "• " item prefix (so a "•" inside body text isn't matched).
func isBulletLine(ln string) bool {
	t := strings.TrimLeft(stripANSI(ln), " ")
	return strings.HasPrefix(t, "• ")
}

// applyLineBg tints the whole visible line with the given hex background,
// re-applying it after each SGR reset (a reset would otherwise clear the
// background mid-line), and pads to width so the tint spans the pane.
func applyLineBg(ln, hex string, width int) string {
	r, g, b := hexToRGB(hex)
	bg := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
	const reset = "\x1b[0m"
	body := strings.ReplaceAll(ln, reset, reset+bg)
	pad := ""
	if v := visibleWidth(ln); v < width {
		pad = strings.Repeat(" ", width-v)
	}
	return bg + body + pad + reset
}

// hexToRGB parses "#RRGGBB" into its components.
func hexToRGB(hex string) (int, int, int) {
	hex = strings.TrimPrefix(hex, "#")
	var r, g, b int
	fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	return r, g, b
}
```

(Merge the new `import` block with `diff.go`'s existing one — `regexp`, `strings`, `fmt`, `github.com/charmbracelet/lipgloss`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... -run TestComposeMarked -v && go vet ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add diff.go diff_test.go style.go
git commit -m "feat: ▸ bullet marker + subtle line flash composition (update pointer)"
```

---

### Task 3: Wire diff + flash into the viewer

**Files:**
- Modify: `ui.go`
- Test: `ui_test.go`

**Interfaces:**
- Consumes: `changedLines`, `composeMarked` (Tasks 1–2).
- Produces:
  - `model` gains `prevBaseline string`, `renderedLines []string`, `changed map[int]bool`, `lineFlash bool`, `noFlash bool`.
  - `func newModel(path string, noFlash bool) model` (signature changed).
  - `type lineFlashOffMsg struct{}`, `func (m *model) recompose()`.

- [ ] **Step 1: Write the failing test**

```go
// ui_test.go — update the helper first:
//   testModel calls newModel(path) → newModel(path, false)
// then append:

func TestUpdatePointerMarksChangedBullet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SIDECAR.md")
	writeFile(t, path, "# T\n\n- alpha\n- beta\n")
	m := testModel(t, path)

	// change one bullet
	writeFile(t, path, "# T\n\n- alpha\n- BETA\n")
	next, _ := m.Update(fileEventMsg{})
	m = next.(model)

	view := stripANSI(m.vp.View())
	if !strings.Contains(view, "▸ BETA") {
		t.Errorf("changed bullet not marked with ▸:\n%s", view)
	}
	if !m.lineFlash {
		t.Error("lineFlash should be set after a content change")
	}
}

func TestUpdatePointerInitialLoadUnmarked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SIDECAR.md")
	writeFile(t, path, "# T\n\n- alpha\n")
	m := testModel(t, path) // first load renders via WindowSizeMsg

	if strings.Contains(stripANSI(m.vp.View()), "▸") {
		t.Errorf("initial load should mark nothing:\n%s", stripANSI(m.vp.View()))
	}
}

func TestUpdatePointerFlashOffKeepsMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SIDECAR.md")
	writeFile(t, path, "# T\n\n- alpha\n")
	m := testModel(t, path)
	writeFile(t, path, "# T\n\n- ALPHA\n")
	next, _ := m.Update(fileEventMsg{})
	m = next.(model)

	// flash on → background present
	if !strings.Contains(m.vp.View(), "\x1b[48;2;") {
		t.Error("expected flash background right after change")
	}
	next, _ = m.Update(lineFlashOffMsg{})
	m = next.(model)
	if strings.Contains(m.vp.View(), "\x1b[48;2;") {
		t.Error("flash background should clear on lineFlashOffMsg")
	}
	if !strings.Contains(stripANSI(m.vp.View()), "▸ ALPHA") {
		t.Error("▸ marker should persist after flash clears")
	}
}

func TestUpdatePointerNoFlashFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SIDECAR.md")
	writeFile(t, path, "# T\n\n- alpha\n")
	m := newModel(path, true) // noFlash
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m = next.(model)
	writeFile(t, path, "# T\n\n- ALPHA\n")
	next, _ = m.Update(fileEventMsg{})
	m = next.(model)
	if strings.Contains(m.vp.View(), "\x1b[48;2;") {
		t.Error("no-flash mode should never inject a background")
	}
	if !strings.Contains(stripANSI(m.vp.View()), "▸ ALPHA") {
		t.Error("▸ marker should still work with --no-flash")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestUpdatePointer -v`
Expected: FAIL — `newModel` arity / `lineFlashOffMsg` undefined

- [ ] **Step 3: Write minimal implementation**

In `ui.go`:

1. Add the message type and timer next to the existing flash ones:

```go
// lineFlashOffMsg clears the subtle post-reload line-background flash.
type lineFlashOffMsg struct{}

const lineFlashDuration = 500 * time.Millisecond

func lineFlashOff() tea.Cmd {
	return tea.Tick(lineFlashDuration, func(time.Time) tea.Msg { return lineFlashOffMsg{} })
}
```

2. Extend the `model` struct and `newModel`:

```go
type model struct {
	// ...existing fields...
	flash bool

	// update pointer
	prevBaseline  string         // content before the last change; diffed vs raw
	renderedLines []string       // cached rendered lines for cheap recompose
	changed       map[int]bool   // changed line indices in the current render
	lineFlash     bool           // subtle line-bg flash active
	noFlash       bool           // --no-flash: suppress the line flash
}

func newModel(path string, noFlash bool) model {
	return model{path: path, noFlash: noFlash}
}
```

3. In `reload`, capture the pre-change baseline and compose the display. Replace
   the tail of `reload` (from computing `contentChanged` through `SetContent`)
   with:

```go
	raw := string(data)
	contentChanged := raw != m.raw
	if !force && !contentChanged {
		return false
	}
	if contentChanged {
		m.prevBaseline = m.raw // old content ("" on first load → no markers)
	}
	m.raw = raw

	rendered, err := renderMarkdown(raw, m.renderWidth())
	if err != nil {
		m.loadErr = err
		m.vp.SetContent(fmt.Sprintf("\n  Render error: %v", err))
		return false
	}
	lines := strings.Split(rendered, "\n")

	var changed map[int]bool
	if m.prevBaseline != "" {
		if base, berr := renderMarkdown(m.prevBaseline, m.renderWidth()); berr == nil {
			changed = changedLines(strings.Split(base, "\n"), lines)
		}
	}
	m.renderedLines = lines
	m.changed = changed

	display := composeMarked(lines, changed, m.lineFlash && !m.noFlash, m.renderWidth())
	offset := m.vp.YOffset
	m.vp.SetContent(display)
	m.vp.SetYOffset(offset)
	return contentChanged
```

4. Add `recompose` (re-set content for the current flash state, scroll preserved):

```go
// recompose re-renders the cached lines for the current flash state without
// re-reading the file — used when only the flash toggles.
func (m *model) recompose() {
	if m.renderedLines == nil {
		return
	}
	display := composeMarked(m.renderedLines, m.changed, m.lineFlash && !m.noFlash, m.renderWidth())
	offset := m.vp.YOffset
	m.vp.SetContent(display)
	m.vp.SetYOffset(offset)
}
```

5. Trigger the line flash on the two reload paths, and handle `lineFlashOffMsg`.
   In the `fileEventMsg` case:

```go
	case fileEventMsg:
		if m.reload(false) {
			m.flash = true
			cmds := []tea.Cmd{flashOff()}
			if !m.noFlash {
				m.lineFlash = true
				m.recompose() // show the flash background immediately
				cmds = append(cmds, lineFlashOff())
			}
			return m, tea.Batch(cmds...)
		}
		return m, nil
```

   In the `tickMsg` case, where `changed := m.reload(false)` currently gates the
   flash, mirror the same block: when `changed` is true, set `m.flash = true`,
   and if `!m.noFlash` set `m.lineFlash = true`, call `m.recompose()`, and add
   `lineFlashOff()` to the batch alongside `tick()` and `flashOff()`.

   Add a new case:

```go
	case lineFlashOffMsg:
		m.lineFlash = false
		m.recompose()
		return m, nil
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./... && go vet ./...`
Expected: PASS (new pointer tests + all existing ui/render tests)

- [ ] **Step 5: Commit**

```bash
git add ui.go ui_test.go
git commit -m "feat: wire update pointer + line flash into the viewer"
```

---

### Task 4: `--no-flash` flag

**Files:**
- Modify: `main.go`
- Test: `main.go` has no unit test harness; covered by `TestUpdatePointerNoFlashFlag` (Task 3) exercising `newModel(path, true)`.

**Interfaces:**
- Consumes: `newModel(path, noFlash)` (Task 3).

- [ ] **Step 1: Parse the flag**

There is no automated test for `main()` arg parsing (it wires `os.Args` to the
tea program). Implement directly, then verify by build + manual run.

The current `main` dispatches subcommands on `os.Args[1]` and then runs the
viewer. Keep the subcommand dispatch exactly as-is (so `init`, `--static`, `-h`,
`-v` are byte-for-byte unchanged), and add `--no-flash` handling only in the
viewer path. Replace the body of `main` with:

```go
func main() {
	// Subcommands dispatch on the first arg, exactly as before.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-h", "--help":
			fmt.Print(help)
			return
		case "-v", "--version":
			fmt.Println("sidecar", version)
			return
		case "-s", "--static":
			os.Exit(runStatic(os.Args[2:]))
		case "init":
			os.Exit(runInit(os.Args[2:]))
		}
	}

	// Viewer mode: an optional file path plus the --no-flash flag, any order.
	path := defaultFile
	noFlash := false
	for _, a := range os.Args[1:] {
		switch a {
		case "--no-flash":
			noFlash = true
		default:
			path = a
		}
	}

	abs, err := filepath.Abs(expandTilde(path))
	if err != nil {
		fmt.Fprintln(os.Stderr, "sidecar:", err)
		os.Exit(1)
	}

	offerCreate(abs) // if missing and interactive, offer to scaffold before opening

	p := tea.NewProgram(newModel(abs, noFlash),
		tea.WithAltScreen(),
	)
	go watchFile(abs, p.Send)

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "sidecar:", err)
		os.Exit(1)
	}
}
```

(`sidecar --no-flash` falls through the subcommand switch — `os.Args[1]` matches
no case — into viewer mode, where the loop sets `noFlash` and leaves `path` at
the default. `sidecar --no-flash foo.md` sets both.)

Verify the subcommand paths still work:

Run: `go build -o /tmp/sidecar . && /tmp/sidecar --help >/dev/null && /tmp/sidecar --version && echo "x" | /tmp/sidecar --static /dev/stdin >/dev/null && echo OK`
Expected: prints the version and `OK` (subcommands unaffected).

- [ ] **Step 2: Update the help text**

Add a line to the `help` const's usage/keys area documenting `--no-flash`:

```
       sidecar --no-flash [file]  disable the subtle change-flash (▸ still shows)
```

- [ ] **Step 3: Build + vet + full suite**

Run: `go build ./... && go vet ./... && gofmt -l . && go test ./...`
Expected: builds, clean, all tests pass.

- [ ] **Step 4: Manual smoke (needs a TTY — run once)**

```bash
go build -o /tmp/sidecar . && cd "$(mktemp -d)" && /tmp/sidecar init >/dev/null 2>&1 || true
```

Then in one pane run `/tmp/sidecar SIDECAR.md`, edit a bullet in another pane,
and confirm: the changed bullet shows a bright `▸`, a subtle background tint
flashes once and settles, and `--no-flash` suppresses only the tint.

- [ ] **Step 5: Commit**

```bash
git add main.go
git commit -m "feat: --no-flash flag to disable the change-flash (update pointer)"
```

---

## Self-review notes

- Spec §"change detection (resize-proof)" → Task 3 `prevBaseline` + re-diff from
  raw on every render. ✓
- Spec §"per-line, no cascade" → Task 1 LCS, `TestChangedLinesInsertionNoCascade`. ✓
- Spec §"`▸` bullets only, width-preserving" → Task 2 `composeMarked`/`isBulletLine`,
  `TestComposeMarkedNonBulletNoMarker`. ✓
- Spec §"subtle one-shot flash, SGR-aware bg" → Task 2 `applyLineBg`, Task 3
  `lineFlashOffMsg`; `TestUpdatePointerFlashOffKeepsMarker`. ✓
- Spec §"`--no-flash`, ▸ still works" → Task 4 flag + Task 3
  `TestUpdatePointerNoFlashFlag`. ✓
- Spec §"initial load marks nothing" → Task 3 empty-baseline guard,
  `TestUpdatePointerInitialLoadUnmarked`. ✓
- Spec §"colors tunable in style.go" → Task 2 consts. ✓
