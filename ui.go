package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// fileEventMsg arrives from the fsnotify watcher after the debounce window.
type fileEventMsg struct{}

// tickMsg drives the "updated Ns ago" clock and the stat-based fallback.
type tickMsg time.Time

// flashOffMsg clears the post-reload status-bar highlight.
type flashOffMsg struct{ gen int }

const flashDuration = 450 * time.Millisecond

func flashOff(gen int) tea.Cmd {
	return tea.Tick(flashDuration, func(time.Time) tea.Msg { return flashOffMsg{gen} })
}

// lineFlashOffMsg clears the subtle post-reload line-background flash.
type lineFlashOffMsg struct{ gen int }

const lineFlashDuration = 500 * time.Millisecond

func lineFlashOff(gen int) tea.Cmd {
	return tea.Tick(lineFlashDuration, func(time.Time) tea.Msg { return lineFlashOffMsg{gen} })
}

type model struct {
	path string

	vp    viewport.Model
	ready bool

	width  int
	height int

	raw         string // last markdown source, to skip no-op re-renders
	fileMissing bool
	loadErr     error

	// mtime/size of the file as of the last load; the 1s tick stats the
	// file and reloads on mismatch, a fallback for missed fsnotify events.
	lastMod  time.Time
	lastSize int64

	// flash briefly highlights the status bar right after a live reload.
	flash bool

	// update pointer
	prevBaseline string // content before the last change; diffed vs raw
	// hasBaseline is true once a prior successful content render exists — it
	// distinguishes "no baseline yet" (very first render, always unmarked)
	// from "baseline was a legitimately empty file" (prevBaseline == "" but
	// still a real prior state to diff against). Set true at the end of the
	// first successful reload, so that reload itself marks nothing.
	hasBaseline   bool
	renderedLines []string     // cached rendered lines for cheap recompose
	changed       map[int]bool // changed line indices in the current render
	lineFlash     bool         // subtle line-bg flash active
	noFlash       bool         // --no-flash: suppress the line flash
	flashGen      int          // bumped on each change; a stale flash-off msg is ignored

	// Section collapse — rendering-only, never written back to the file.
	// board is the last successful parseBoard result; collapsed is keyed by
	// BoardSection.Label and persists in-memory across reloads within a run
	// (see seedDefaults, collapse.go). cursor is which section Tab/Shift+Tab
	// is on, -1 meaning "no board parsed" (headerLines is then empty too, so
	// applyCursorHighlight is a no-op regardless). headerLines maps section
	// index to its rendered line index (sectionHeaderLines, diff.go).
	board       Board
	collapsed   map[string]bool
	cursor      int
	headerLines []int
}

func newModel(path string, noFlash bool) model {
	return model{path: path, noFlash: noFlash, collapsed: map[string]bool{}, cursor: -1}
}

func (m model) Init() tea.Cmd {
	return tick()
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			if m.reload(true) {
				m.flashGen++
				gen := m.flashGen
				m.flash = true
				cmds := []tea.Cmd{flashOff(gen)}
				if !m.noFlash {
					m.lineFlash = true
					m.recompose()
					cmds = append(cmds, lineFlashOff(gen))
				}
				return m, tea.Batch(cmds...)
			}
			return m, nil
		case "g", "home":
			m.vp.GotoTop()
			return m, nil
		case "G", "end":
			m.vp.GotoBottom()
			return m, nil
		case "tab":
			m.moveCursor(1)
			return m, nil
		case "shift+tab":
			m.moveCursor(-1)
			return m, nil
		case "enter", " ":
			if len(m.board.Sections) == 0 {
				// No board parsed: fall through to the viewport so " "
				// keeps its default page-down binding.
				break
			}
			m.toggleCursor()
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if !m.ready {
			m.vp = viewport.New(m.width, m.height-1)
			m.ready = true
		} else {
			m.vp.Width = m.width
			m.vp.Height = m.height - 1
		}
		m.reload(true) // width changed: re-render at the new wrap width
		return m, nil

	case fileEventMsg:
		if m.reload(false) {
			m.flashGen++
			gen := m.flashGen
			m.flash = true
			cmds := []tea.Cmd{flashOff(gen)}
			if !m.noFlash {
				m.lineFlash = true
				m.recompose() // show the flash background immediately
				cmds = append(cmds, lineFlashOff(gen))
			}
			return m, tea.Batch(cmds...)
		}
		return m, nil

	case tickMsg:
		// Stat fallback: catch changes fsnotify missed.
		changed := false
		if st, err := os.Stat(m.path); err != nil {
			if !m.fileMissing {
				changed = m.reload(false)
			}
		} else if m.fileMissing || !st.ModTime().Equal(m.lastMod) || st.Size() != m.lastSize {
			changed = m.reload(false)
		}
		if changed {
			m.flashGen++
			gen := m.flashGen
			m.flash = true
			cmds := []tea.Cmd{tick(), flashOff(gen)}
			if !m.noFlash {
				m.lineFlash = true
				m.recompose()
				cmds = append(cmds, lineFlashOff(gen))
			}
			return m, tea.Batch(cmds...)
		}
		return m, tick()

	case flashOffMsg:
		if msg.gen != m.flashGen {
			return m, nil
		}
		m.flash = false
		return m, nil

	case lineFlashOffMsg:
		if msg.gen != m.flashGen {
			return m, nil
		}
		m.lineFlash = false
		m.recompose()
		return m, nil
	}

	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

// reload re-reads the file and re-renders, preserving the scroll position
// (clamped if the content shrank). Rendering happens off-screen into a
// string; the viewport swap is a single frame, so no flicker. It returns
// true when the file's content actually changed (used to trigger the reload
// flash) — a forced re-render at the same content (e.g. resize) returns
// false so resizing doesn't flash.
func (m *model) reload(force bool) (changed bool) {
	if !m.ready {
		return false
	}
	data, err := os.ReadFile(m.path)
	if err != nil {
		m.fileMissing = os.IsNotExist(err)
		m.loadErr = err
		m.lastMod, m.lastSize = time.Time{}, 0
		m.raw = ""
		if m.fileMissing {
			m.vp.SetContent(waitingView(m.path))
		} else {
			m.vp.SetContent(fmt.Sprintf("\n  Error reading %s:\n  %v", m.path, err))
		}
		m.vp.GotoTop()
		m.renderedLines = nil
		m.changed = nil
		m.lineFlash = false
		m.hasBaseline = false
		return false
	}
	if st, err := os.Stat(m.path); err == nil {
		m.lastMod, m.lastSize = st.ModTime(), st.Size()
	}
	m.fileMissing = false
	m.loadErr = nil

	raw := string(data)
	contentChanged := raw != m.raw
	if !force && !contentChanged {
		return false
	}
	if contentChanged {
		m.prevBaseline = m.raw // old content ("" on first load → no markers)
	}
	m.raw = raw

	// First successful render — and after an error/missing-file recovery, where
	// hasBaseline was reset — seed the baseline to the content itself, so a
	// forced re-render (resize, r) before any real change diffs against itself
	// and marks nothing.
	if !m.hasBaseline {
		m.prevBaseline = raw
	}

	// Section collapse is rendering-only: parse the board, seed any new
	// section labels' default collapse state, clamp the cursor to the
	// current section count, then rewrite what actually reaches glamour.
	board, boardOK := parseBoard(raw)
	if boardOK {
		m.board = board
		seedDefaults(board, m.collapsed)
		if m.cursor < 0 {
			m.cursor = 0
		} else if m.cursor >= len(board.Sections) {
			m.cursor = len(board.Sections) - 1
		}
	} else {
		m.board = Board{}
		m.cursor = -1
	}
	displayRaw := raw
	if boardOK {
		displayRaw = applyCollapse(raw, board, m.collapsed)
	}

	rendered, err := renderMarkdown(displayRaw, m.renderWidth())
	if err != nil {
		m.loadErr = err
		m.vp.SetContent(fmt.Sprintf("\n  Render error: %v", err))
		m.renderedLines = nil
		m.changed = nil
		m.lineFlash = false
		m.hasBaseline = false
		return false
	}
	lines := strings.Split(rendered, "\n")
	m.headerLines = sectionHeaderLines(lines)

	var changedMap map[int]bool
	if m.hasBaseline {
		// Deliberate degrade: if the baseline fails to render we show no
		// markers this pass rather than surface an error — the content render
		// above already succeeded. The baseline is rendered through the same
		// collapse rewrite, using the *current* collapsed state — collapse is
		// a live viewer setting, not a property of any one file revision.
		baseDisplay := m.prevBaseline
		if baseBoard, ok := parseBoard(m.prevBaseline); ok {
			baseDisplay = applyCollapse(m.prevBaseline, baseBoard, m.collapsed)
		}
		if base, berr := renderMarkdown(baseDisplay, m.renderWidth()); berr == nil {
			changedMap = changedLines(strings.Split(base, "\n"), lines)
		}
	}
	m.renderedLines = lines
	m.changed = changedMap

	display := m.compose()
	offset := m.vp.YOffset
	m.vp.SetContent(display)
	m.vp.SetYOffset(offset)
	m.hasBaseline = true
	return contentChanged
}

// compose renders the current cached lines through both overlays — the
// changed/flash marking (composeMarked) and the section-cursor highlight
// (applyCursorHighlight) — at the current flash and cursor state.
func (m *model) compose() string {
	display := composeMarked(m.renderedLines, m.changed, m.lineFlash && !m.noFlash, m.renderWidth())
	return applyCursorHighlight(display, m.headerLines, m.cursor, m.renderWidth())
}

// recompose re-renders the cached lines for the current flash state without
// re-reading the file — used when only the flash toggles.
func (m *model) recompose() {
	if m.renderedLines == nil || m.fileMissing || m.loadErr != nil {
		return
	}
	display := m.compose()
	offset := m.vp.YOffset
	m.vp.SetContent(display)
	m.vp.SetYOffset(offset)
}

// rerenderCollapse re-renders m.raw with the current m.collapsed state,
// without touching the baseline or triggering the reload flash — toggling
// a section isn't a "the file changed on disk" event. A no-op before the
// first successful load.
func (m *model) rerenderCollapse() {
	if !m.ready || m.fileMissing || m.loadErr != nil || len(m.board.Sections) == 0 {
		return
	}
	displayRaw := applyCollapse(m.raw, m.board, m.collapsed)
	rendered, err := renderMarkdown(displayRaw, m.renderWidth())
	if err != nil {
		return // m.raw already rendered fine on the last successful reload
	}
	m.renderedLines = strings.Split(rendered, "\n")
	m.headerLines = sectionHeaderLines(m.renderedLines)
	m.changed = nil
	offset := m.vp.YOffset
	m.vp.SetContent(m.compose())
	m.vp.SetYOffset(offset)
}

// moveCursor steps the section cursor by delta (±1), wrapping at both
// ends, then re-tints the highlighted header and scrolls it into view. A
// no-op when no board is parsed (cursor stays -1, see reload).
func (m *model) moveCursor(delta int) {
	n := len(m.board.Sections)
	if n == 0 {
		return
	}
	m.cursor = ((m.cursor+delta)%n + n) % n
	m.recompose()
	m.scrollToCursor()
}

// toggleCursor flips collapsed[label] for the section at the cursor and
// re-renders. A no-op when no board is parsed.
func (m *model) toggleCursor() {
	if len(m.board.Sections) == 0 {
		return
	}
	label := m.board.Sections[m.cursor].Label
	m.collapsed[label] = !m.collapsed[label]
	m.rerenderCollapse()
}

// scrollToCursor nudges the viewport's YOffset just enough to bring the
// cursor's header line into view, with a 1-line margin — it does not
// re-center the pane. A no-op when the cursor has no corresponding
// rendered header line yet.
func (m *model) scrollToCursor() {
	if m.cursor < 0 || m.cursor >= len(m.headerLines) {
		return
	}
	const margin = 1
	target := m.headerLines[m.cursor]
	top := m.vp.YOffset
	bottom := top + m.vp.Height - 1
	switch {
	case target < top+margin:
		m.vp.SetYOffset(max(0, target-margin))
	case target > bottom-margin:
		m.vp.SetYOffset(target - m.vp.Height + 1 + margin)
	}
}

// renderWidth is the markdown wrap width: pane width minus 2, never wider
// than the pane.
func (m model) renderWidth() int {
	return m.width - 2
}

var (
	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorStatusFg)).
			Background(lipgloss.Color(colorStatusBg))
	statusNameStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorStatusHi)).
			Background(lipgloss.Color(colorStatusBg)).
			Bold(true)
	// statusFlashStyle briefly replaces the filename badge right after a
	// live reload — black on amber, so a change catches the eye.
	statusFlashStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(colorH1Fg)).
				Background(lipgloss.Color(colorHeading)).
				Bold(true)

	waitBadgeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colorH1Fg)).
			Background(lipgloss.Color(colorH1Bg)).
			Bold(true)
	waitAccentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorHeading)).Bold(true)
	waitDimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(colorStatusFg))
)

// waitingView is the placeholder shown before the file exists — a small
// styled header so an empty pane still looks intentional, plus which file
// it's watching for.
func waitingView(path string) string {
	var b strings.Builder
	b.WriteString("\n  ")
	b.WriteString(waitBadgeStyle.Render(" 🚗 sidecar "))
	b.WriteString("  ")
	b.WriteString(waitDimStyle.Render("your AI's live scratchpad"))
	b.WriteString("\n\n  ")
	b.WriteString(waitAccentStyle.Render("⧗ waiting for ") + filepath.Base(path))
	b.WriteString("\n  ")
	b.WriteString(waitDimStyle.Render("  it renders the moment it appears — run `sidecar init` to make one"))
	b.WriteString("\n\n  ")
	b.WriteString(waitDimStyle.Render(path))
	return b.String()
}

func (m model) View() string {
	if !m.ready {
		return "loading…"
	}
	return m.vp.View() + "\n" + m.statusBar()
}

func (m model) statusBar() string {
	name := filepath.Base(m.path)

	var updated string
	switch {
	case m.fileMissing:
		updated = "waiting for file"
	case m.loadErr != nil:
		updated = "error"
	case m.lastMod.IsZero():
		updated = "…"
	default:
		updated = "updated " + humanSince(time.Since(m.lastMod)) + " ago"
	}

	left := " 🚗 " + name + " "
	info := "· " + updated
	pct := fmt.Sprintf(" %3.0f%% ", m.vp.ScrollPercent()*100)

	pad := m.width - visibleWidth(left) - visibleWidth(info) - visibleWidth(pct)
	if pad < 0 {
		info = truncateTo(info, max(0, m.width-visibleWidth(left)-visibleWidth(pct)))
		pad = max(0, m.width-visibleWidth(left)-visibleWidth(info)-visibleWidth(pct))
	}

	nameStyle := statusNameStyle
	if m.flash {
		nameStyle = statusFlashStyle
	}
	return nameStyle.Render(left) +
		statusStyle.Render(info+strings.Repeat(" ", pad)+pct)
}

func truncateTo(s string, w int) string {
	if w <= 0 {
		return ""
	}
	var b strings.Builder
	width := 0
	for _, r := range s {
		rw := visibleWidth(string(r))
		if width+rw > w {
			break
		}
		b.WriteRune(r)
		width += rw
	}
	return b.String()
}

func humanSince(d time.Duration) string {
	switch {
	case d < time.Second:
		return "0s"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		return fmt.Sprintf("%dh%02dm", h, int(d.Minutes())-60*h)
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
