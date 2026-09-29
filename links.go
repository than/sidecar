// links.go — clickable links. With mouse capture on, the terminal no longer
// handles a click on an OSC 8 hyperlink, so the viewer does: it finds the
// link under the pointer in the rendered line, paints it on hover, shows the
// full address in the status bar, and opens it on a click.
package main

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var linkHotStyle = lipgloss.NewStyle().Bold(true).Underline(true).
	Foreground(lipgloss.Color("#101010")).Background(lipgloss.Color("#61AFEF"))

// linkSpan is a hyperlink on a rendered line: the display columns it covers
// (from inclusive, to exclusive) and its target.
type linkSpan struct {
	from, to int
	url      string
}

// linkSpans finds the OSC 8 hyperlinks on a rendered line, in display
// columns. It understands BEL and ESC \ terminators and skips styling
// sequences, so the columns match what the terminal draws.
func linkSpans(line string) []linkSpan {
	var spans []linkSpan
	col, open, target := 0, -1, ""
	for i := 0; i < len(line); {
		if line[i] == 0x1b && i+1 < len(line) {
			switch line[i+1] {
			case ']':
				end, next := -1, -1
				for j := i + 2; j < len(line); j++ {
					if line[j] == 0x07 {
						end, next = j, j+1
						break
					}
					if line[j] == 0x1b && j+1 < len(line) && line[j+1] == '\\' {
						end, next = j, j+2
						break
					}
				}
				if end < 0 {
					return spans
				}
				if payload := line[i+2 : end]; strings.HasPrefix(payload, "8;") {
					parts := strings.SplitN(payload, ";", 3)
					switch {
					case len(parts) == 3 && parts[2] != "":
						open, target = col, parts[2]
					case open >= 0:
						spans = append(spans, linkSpan{open, col, target})
						open = -1
					}
				}
				i = next
				continue
			case '[':
				j := i + 2
				for j < len(line) && !(line[j] >= 0x40 && line[j] <= 0x7e) {
					j++
				}
				i = j + 1
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		col += ansi.StringWidth(string(r))
		i += size
	}
	return spans
}

// linkAt returns the link covering display column x on a rendered line.
func linkAt(line string, x int) (linkSpan, bool) {
	for _, s := range linkSpans(line) {
		if x >= s.from && x < s.to {
			return s, true
		}
	}
	return linkSpan{}, false
}

// linkAllowed limits what a click may open. A board is written by an agent
// pasting arbitrary text, so only web and mail links are opened — never a
// file: path or an app's custom scheme.
func linkAllowed(target string) bool {
	u, err := url.Parse(target)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto":
		return true
	}
	return false
}

// openURL hands a link to the OS. A variable so tests do not launch a browser.
var openURL = func(target string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", target).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
	}
	return exec.Command("xdg-open", target).Start()
}

func (m *model) openLink(target string) {
	if !linkAllowed(target) {
		m.notice = "won't open that kind of link"
		return
	}
	if err := openURL(target); err != nil {
		m.notice = fmt.Sprintf("couldn't open link: %v", err)
		return
	}
	m.notice = "opened " + target
}

// paintLink turns the link's cells solid on a rendered line.
func paintLink(line string, s linkSpan) string {
	w := visibleWidth(line)
	return ansi.Cut(line, 0, s.from) + linkHotStyle.Render(stripANSI(ansi.Cut(line, s.from, s.to))) + ansi.Cut(line, s.to, w)
}
