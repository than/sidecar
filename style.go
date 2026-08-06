package main

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	reflowansi "github.com/muesli/reflow/ansi"
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
)

func ptr[T any](v T) *T { return &v }

// osc8RE matches an OSC 8 hyperlink open ("\x1b]8;;URL\x07") or close
// ("\x1b]8;;\x07") sequence — BEL-terminated, as emitted by osc8Open in
// linkify.go. Shared by visibleWidth here and stripANSI in diff.go so both
// treat the hyperlink target as invisible, matching what a terminal does.
var osc8RE = regexp.MustCompile("\x1b\\]8;;[^\x07]*\x07")

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

// renderMarkdown renders raw markdown at the given width (already reduced
// from the pane width by the caller). Output is post-processed to guarantee
// the hard requirements: no trailing-space padding, at most one blank line
// between blocks, no leading/trailing blank runs.
func renderMarkdown(rawIn string, width int) (string, error) {
	if width < 10 {
		width = 10
	}
	// Bare URLs wider than width can't survive glamour's word-wrap intact —
	// it force-breaks long "words" mid-character (issue #15). Shrink any
	// bare-URL-only line to fit before rendering, then re-attach the full
	// URL as an OSC 8 hyperlink target after rendering so the visible,
	// possibly-truncated text still opens the right place.
	raw, truncations := truncateBareURLs(rawIn, width, nil)
	out, err := glamourRender(raw, width)
	if err != nil {
		return "", err
	}
	out = tidy(out)
	out, unresolved := linkifyTruncations(out, truncations)
	if len(unresolved) == 0 {
		return out, nil
	}

	// A truncation's display text couldn't be found post-render — the
	// reserve estimate under-budgeted (e.g. an indent shape truncateBareURLs
	// didn't anticipate) and glamour force-wrapped it after all. Truncated,
	// inert text with no hyperlink would be worse than the pre-fix bug (at
	// least the old broken fragments were plain URL text a terminal's own
	// regex might partially match); fall back to rendering those specific
	// URLs untruncated instead — same behavior as before this fix, only for
	// the lines that need it — rather than risk a second miss compounding
	// the first.
	skip := make(map[string]bool, len(unresolved))
	for _, t := range unresolved {
		skip[t.full] = true
	}
	raw, truncations = truncateBareURLs(rawIn, width, skip)
	out, err = glamourRender(raw, width)
	if err != nil {
		return "", err
	}
	out = tidy(out)
	out, _ = linkifyTruncations(out, truncations) // best-effort; any further miss just stays untruncated
	return out, nil
}

// glamourRender runs raw markdown through glamour at the given width, with
// this file's style and truecolor forced on.
func glamourRender(raw string, width int) (string, error) {
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
	return r.Render(raw)
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

// visibleWidth is the printable cell width of a line, ignoring ANSI codes.
//
// reflow's ANSI-aware width counter only recognizes CSI sequences
// ("\x1b[...letter") as escapes; it has no notion of OSC 8 hyperlinks
// ("\x1b]8;;URL\x07text\x1b]8;;\x07") and would treat the URL bytes inside
// the escape as visible text — any lowercase letter in the URL looks like a
// premature CSI terminator to it. Strip OSC 8 sequences first so the target
// URL never leaks into the width count.
func visibleWidth(line string) int {
	return reflowansi.PrintableRuneWidth(osc8RE.ReplaceAllString(line, ""))
}
