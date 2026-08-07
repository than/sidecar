package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"
)

// Palette. All colors are hex — never 256-palette indexes, which Ghostty
// remaps (palette "light blue 111" rendered royal blue).
const (
	// Links: teal leaning blue-cyan. #56d4bd read as green — this is bluer.
	// Tune this one const to adjust every link.
	colorLink = "#4EC9E5"

	colorText        = "#D0D0D0" // body text
	colorHeading     = "#D19A66" // muted amber, h2/h3
	colorH1Fg        = "#000000" // h1 badge text
	colorH1Bg        = "#AF87FF" // h1 badge background, light lavender
	colorCodeFg      = "#FF5F5F" // inline code
	colorCodeBg      = "#303030" // inline code background
	colorCodeDim     = "#808080" // fenced code blocks
	colorRule        = "#585858" // horizontal rules
	colorStatusFg    = "#8A8F98" // status bar text
	colorStatusHi    = "#C8CCD4" // status bar filename
	colorStatusBg    = "#26262A" // status bar background
	colorUpdated     = "#5FE3A1" // bright ▸ marking a changed line
	colorFlashLineBg = "#2A2A33" // subtle bg lightening on a just-changed line
	colorCursorBg    = "#3A3550" // section-cursor highlight, distinct from the reload flash
)

func ptr[T any](v T) *T { return &v }

// styleConfig is a compact glamour style: at most one blank line between
// blocks, zero margins (glamour margins pad every line with trailing spaces
// to the wrap width, which fakes double-spacing in a narrow pane).
func styleConfig() ansi.StyleConfig {
	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Color: ptr(colorText)},
			Margin:         ptr(uint(0)),
		},
		BlockQuote: ansi.StyleBlock{
			Indent:      ptr(uint(1)),
			IndentToken: ptr("│ "),
		},
		Paragraph: ansi.StyleBlock{},
		List: ansi.StyleList{
			LevelIndent: 2,
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockSuffix: "\n",
				Color:       ptr(colorHeading),
				Bold:        ptr(true),
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix:          " ",
				Suffix:          " ",
				Color:           ptr(colorH1Fg),
				BackgroundColor: ptr(colorH1Bg),
				Bold:            ptr(true),
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Prefix: "▍ "},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{Prefix: "┃ "},
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "• ",
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ",
		},
		Task: ansi.StyleTask{
			Ticked:   "✓ ",
			Unticked: "□ ",
		},
		Emph:   ansi.StylePrimitive{Italic: ptr(true)},
		Strong: ansi.StylePrimitive{Bold: ptr(true)}, // bold only, default fg
		HorizontalRule: ansi.StylePrimitive{
			Color:  ptr(colorRule),
			Format: "\n────────────────────\n",
		},
		Link: ansi.StylePrimitive{
			Color:     ptr(colorLink),
			Underline: ptr(true),
		},
		LinkText: ansi.StylePrimitive{
			Color: ptr(colorLink),
			Bold:  ptr(true),
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color:           ptr(colorCodeFg),
				BackgroundColor: ptr(colorCodeBg),
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{Color: ptr(colorCodeDim)},
				Margin:         ptr(uint(0)),
			},
		},
		Table: ansi.StyleTable{
			CenterSeparator: ptr("┼"),
			ColumnSeparator: ptr("│"),
			RowSeparator:    ptr("─"),
		},
	}
}

// bareURLLine matches a markdown line that is nothing but a bare URL — the
// board convention for links (see CLAUDE.md: "bare URLs, each on its own
// line") — whether it's an indented continuation line under a bullet or a
// top-level list item in its own right.
var bareURLLine = regexp.MustCompile(`^([ \t]*(?:[-*+]\s+)?)(https?://\S+)([ \t]*)$`)

// fenceLine matches a fenced-code-block delimiter (``` or ~~~, 3+ of the
// same character). Bare URLs inside a fence are content the user typed
// verbatim and are never word-wrapped by glamour in the first place, so
// stashBareURLs leaves them alone.
var fenceLine = regexp.MustCompile("^[ \t]*(```+|~~~+)")

// urlPlaceholder is a short, markdown-inert stand-in for a stashed URL. It
// uses the ASCII unit separator as a delimiter so it can never collide with
// real board text, and stays well under any realistic wrap width — short
// enough that glamour's word-wrap never has a reason to touch it.
func urlPlaceholder(i int) string {
	return fmt.Sprintf("\x1fU%d\x1f", i)
}

// stashBareURLs replaces every bare-URL-only line with a short placeholder,
// returning the stashed URLs in order (skipping lines inside fenced code
// blocks). Glamour's word-wrap hard-splits a long link token mid-URL once it
// exceeds the wrap width — it treats the autolink's rendered ANSI run
// differently from plain text and force-breaks it instead of pushing the
// whole word to the next line — so the placeholder keeps such lines out of
// that path entirely. restoreBareURLs puts the real, styled, hyperlinked URL
// back after rendering.
func stashBareURLs(raw string) (string, []string) {
	lines := strings.Split(raw, "\n")
	var urls []string
	var fenceChar byte
	for i, line := range lines {
		if m := fenceLine.FindStringSubmatch(line); m != nil {
			c := m[1][0]
			switch fenceChar {
			case 0:
				fenceChar = c
			case c:
				fenceChar = 0
			}
			continue
		}
		if fenceChar != 0 {
			continue
		}
		if m := bareURLLine.FindStringSubmatch(line); m != nil {
			urls = append(urls, m[2])
			lines[i] = m[1] + urlPlaceholder(len(urls)-1)
		}
	}
	return strings.Join(lines, "\n"), urls
}

// oscOpen/oscClose delimit an OSC 8 hyperlink. BEL-terminated rather than
// ST (ESC \\): this codebase's other ANSI helpers only recognize a bare
// ESC[0m-style reset, and BEL is a single unambiguous byte to bound a
// terminator scan on.
const oscOpen = "\x1b]8;;"
const oscBEL = "\x07"
const oscClose = oscOpen + oscBEL

// oscTarget encodes url for safe use as an OSC 8 target: bytes that could
// terminate the escape early (control bytes, DEL) or aren't valid in a URI
// (anything non-ASCII) are percent-encoded rather than rejected, so a
// pathological or non-ASCII URL still gets a working, complete link instead
// of silently losing its hyperlink.
func oscTarget(url string) string {
	var b strings.Builder
	for i := 0; i < len(url); i++ {
		c := url[i]
		if c < 0x20 || c == 0x7f || c >= 0x80 {
			fmt.Fprintf(&b, "%%%02X", c)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// hyperlink wraps display in an OSC 8 hyperlink pointing at target. display
// carries its own SGR styling; the OSC 8 escapes only add the link.
func hyperlink(target, display string) string {
	return oscOpen + oscTarget(target) + oscBEL + display + oscClose
}

// elideURL returns url unchanged if it fits within budget cells, otherwise
// cuts it (cell-width aware, not byte- or rune-count aware) to make room for
// a trailing ellipsis whose own width is measured rather than assumed to be
// one cell. budget <= 0 is a pathological deeply-nested-bullet-in-a-tiny-pane
// case; it still returns a single ellipsis rather than nothing, since an
// empty display text would be an invisible — but still clickable — link.
func elideURL(url string, budget int) string {
	if runewidth.StringWidth(url) <= budget {
		return url
	}
	ellipsisWidth := runewidth.RuneWidth('…')
	keep := budget - ellipsisWidth
	if keep <= 0 {
		return "…"
	}
	var b strings.Builder
	w := 0
	for _, r := range url {
		rw := runewidth.RuneWidth(r)
		if w+rw > keep {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	b.WriteRune('…')
	return b.String()
}

// restoreBareURLs swaps each placeholder back for its real URL, wrapped in
// an OSC 8 hyperlink and styled like glamour's own Link (colorLink,
// underlined). The visible text is elided to fit whatever width remains on
// its line — measured from the actual rendered prefix (bullet, indent,
// nesting), not guessed — but the hyperlink target always carries the full,
// untruncated URL, so the link opens the right place regardless of how much
// of it is shown. Each URL has its own index-keyed placeholder, so two URLs
// that happen to elide to identical visible text can never cross-link —
// there's nothing to search for and collide on.
func restoreBareURLs(rendered string, urls []string, width int) string {
	if len(urls) == 0 {
		return rendered
	}
	lines := strings.Split(rendered, "\n")
	for i, url := range urls {
		ph := urlPlaceholder(i)
		for li, line := range lines {
			idx := strings.Index(line, ph)
			if idx < 0 {
				continue
			}
			prefix := line[:idx]
			budget := width - visibleWidth(prefix)
			display := elideURL(url, budget)
			styled := termenv.String(display).Foreground(termenv.TrueColor.Color(colorLink)).Underline().String()
			lines[li] = prefix + hyperlink(url, styled)
			break
		}
	}
	return strings.Join(lines, "\n")
}

// renderMarkdown renders raw markdown at the given width (already reduced
// from the pane width by the caller). Output is post-processed to guarantee
// the hard requirements: no trailing-space padding, at most one blank line
// between blocks, no leading/trailing blank runs.
func renderMarkdown(raw string, width int) (string, error) {
	if width < 10 {
		width = 10
	}
	raw, urls := stashBareURLs(raw)
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(styleConfig()),
		glamour.WithWordWrap(width),
		// Force truecolor so hex colors survive; piped/degraded profiles
		// were how glow washed out.
		glamour.WithColorProfile(termenv.TrueColor),
	)
	if err != nil {
		return "", err
	}
	out, err := r.Render(raw)
	if err != nil {
		return "", err
	}
	out = restoreBareURLs(out, urls, width)
	return tidy(out), nil
}

// tidy strips trailing-space padding and collapses runs of blank lines to a
// single blank line.
func tidy(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	var out []string
	blanks := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " ")
		if visibleWidth(line) == 0 {
			blanks++
			if blanks > 1 {
				continue
			}
			line = ""
		} else {
			blanks = 0
		}
		out = append(out, line)
	}
	// Drop leading/trailing blank lines.
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

// visibleWidth is the printable cell width of a line, ignoring ANSI codes —
// SGR and OSC 8 hyperlinks alike. x/ansi's parser understands OSC; the
// muesli/reflow width counter this used to call does not (it only
// recognizes CSI's `[0-9;]*[A-Za-z]` terminator), so an OSC 8-wrapped line
// would otherwise measure with the link target counted as visible text.
func visibleWidth(line string) int {
	return xansi.StringWidth(line)
}
