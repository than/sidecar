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
type flashOffMsg struct{}

const flashDuration = 450 * time.Millisecond

func flashOff() tea.Cmd {
	return tea.Tick(flashDuration, func(time.Time) tea.Msg { return flashOffMsg{} })
}

// lineFlashOffMsg clears the subtle post-reload line-background flash.
type lineFlashOffMsg struct{}

const lineFlashDuration = 500 * time.Millisecond

func lineFlashOff() tea.Cmd {
	return tea.Tick(lineFlashDuration, func(time.Time) tea.Msg { return lineFlashOffMsg{} })
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
	prevBaseline  string       // content before the last change; diffed vs raw
	renderedLines []string     // cached rendered lines for cheap recompose
	changed       map[int]bool // changed line indices in the current render
	lineFlash     bool         // subtle line-bg flash active
	noFlash       bool         // --no-flash: suppress the line flash
}

func newModel(path string, noFlash bool) model {
	return model{path: path, noFlash: noFlash}
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
			m.reload(true)
			return m, nil
		case "g", "home":
			m.vp.GotoTop()
			return m, nil
		case "G", "end":
			m.vp.GotoBottom()
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
			m.flash = true
			cmds := []tea.Cmd{tick(), flashOff()}
			if !m.noFlash {
				m.lineFlash = true
				m.recompose()
				cmds = append(cmds, lineFlashOff())
			}
			return m, tea.Batch(cmds...)
		}
		return m, tick()

	case flashOffMsg:
		m.flash = false
		return m, nil

	case lineFlashOffMsg:
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

	rendered, err := renderMarkdown(raw, m.renderWidth())
	if err != nil {
		m.loadErr = err
		m.vp.SetContent(fmt.Sprintf("\n  Render error: %v", err))
		return false
	}
	lines := strings.Split(rendered, "\n")

	var changedMap map[int]bool
	if m.prevBaseline != "" {
		if base, berr := renderMarkdown(m.prevBaseline, m.renderWidth()); berr == nil {
			changedMap = changedLines(strings.Split(base, "\n"), lines)
		}
	}
	m.renderedLines = lines
	m.changed = changedMap

	display := composeMarked(lines, changedMap, m.lineFlash && !m.noFlash, m.renderWidth())
	offset := m.vp.YOffset
	m.vp.SetContent(display)
	m.vp.SetYOffset(offset)
	return contentChanged
}

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
