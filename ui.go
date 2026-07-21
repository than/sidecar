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
}

func newModel(path string) model {
	return model{path: path}
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
		m.reload(false)
		return m, nil

	case tickMsg:
		// Stat fallback: catch changes fsnotify missed.
		if st, err := os.Stat(m.path); err != nil {
			if !m.fileMissing {
				m.reload(false)
			}
		} else if m.fileMissing || !st.ModTime().Equal(m.lastMod) || st.Size() != m.lastSize {
			m.reload(false)
		}
		return m, tick()
	}

	var cmd tea.Cmd
	m.vp, cmd = m.vp.Update(msg)
	return m, cmd
}

// reload re-reads the file and re-renders, preserving the scroll position
// (clamped if the content shrank). Rendering happens off-screen into a
// string; the viewport swap is a single frame, so no flicker.
func (m *model) reload(force bool) {
	if !m.ready {
		return
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
		return
	}
	if st, err := os.Stat(m.path); err == nil {
		m.lastMod, m.lastSize = st.ModTime(), st.Size()
	}
	m.fileMissing = false
	m.loadErr = nil

	raw := string(data)
	if !force && raw == m.raw {
		return
	}
	m.raw = raw

	rendered, err := renderMarkdown(raw, m.renderWidth())
	if err != nil {
		m.loadErr = err
		m.vp.SetContent(fmt.Sprintf("\n  Render error: %v", err))
		return
	}

	offset := m.vp.YOffset // preserve scroll; if at top this is 0 and stays 0
	m.vp.SetContent(rendered)
	m.vp.SetYOffset(offset) // viewport clamps to the new content height
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

	left := " " + name + " "
	info := "· " + updated
	pct := fmt.Sprintf(" %3.0f%% ", m.vp.ScrollPercent()*100)

	pad := m.width - visibleWidth(left) - visibleWidth(info) - visibleWidth(pct)
	if pad < 0 {
		info = truncateTo(info, max(0, m.width-visibleWidth(left)-visibleWidth(pct)))
		pad = max(0, m.width-visibleWidth(left)-visibleWidth(info)-visibleWidth(pct))
	}

	return statusNameStyle.Render(left) +
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
