package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	xansi "github.com/charmbracelet/x/ansi"
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
// board convention for links (see README.md's rendering-style section:
// "Bare URLs ... render as real OSC 8 hyperlinks") — whether it's an
// indented continuation line under a bullet, a bulleted top-level item, or
// an ordered-list item (`1.`/`1)`). Groups: 1 = leading indent, 2 =
// optional list marker (with its trailing space), 3 = the URL, 4 = trailing
// whitespace, \r included so a CRLF board doesn't
// silently skip the fix.
var bareURLLine = regexp.MustCompile(`^([ \t]*)((?:(?:[-*+]|\d+[.)])\s+)?)(https?://\S+)([ \t\r]*)$`)

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

// residualPlaceholder matches any urlPlaceholder token — a fallback sweep
// for the case restoreBareURLs' own placeholder search doesn't find one.
var residualPlaceholder = regexp.MustCompile(`\x1fU\d+\x1f`)

// listMarkerPrefix matches a line beginning with a list marker, regardless
// of what follows — used to tell an indented code block's opening line
// apart from a list item that merely reaches the same 4-space indent.
var listMarkerPrefix = regexp.MustCompile(`^[ \t]*(?:[-*+]|\d+[.)])\s+`)

// leadingIndent returns line's leading run of spaces/tabs.
func leadingIndent(line string) string {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return line[:i]
}

// hasCodeIndent reports whether indent (spaces/tabs only) is deep enough to
// open or continue a CommonMark indented code block: 4+ spaces, or any tab.
func hasCodeIndent(indent string) bool {
	return strings.Contains(indent, "\t") || len(indent) >= 4
}

// stashBareURLs replaces every bare-URL-only line with a short placeholder,
// returning the stashed URLs in order (skipping lines inside fenced or
// indented code blocks). Glamour's word-wrap hard-splits a long link token
// mid-URL once it exceeds the wrap width — it treats the autolink's
// rendered ANSI run differently from plain text and force-breaks it instead
// of pushing the whole word to the next line — so the placeholder keeps
// such lines out of that path entirely. restoreBareURLs puts the real,
// styled, hyperlinked URL back after rendering.
func stashBareURLs(raw string) (string, []string) {
	lines := strings.Split(raw, "\n")
	var urls []string
	var fenceChar byte
	var fenceLen int
	inIndentedCode := false
	prevBlank := true // document start counts as a boundary, same as CommonMark
	for i, line := range lines {
		wasPrevBlank := prevBlank
		blank := strings.TrimSpace(line) == ""
		prevBlank = blank

		if m := fenceLine.FindStringSubmatch(line); m != nil {
			c, n := m[1][0], len(m[1])
			switch {
			case fenceChar == 0:
				fenceChar, fenceLen = c, n
			case c == fenceChar && n >= fenceLen:
				// Per CommonMark, a fence only closes on a run of the same
				// character at least as long as the one that opened it —
				// a shorter or different-character run (e.g. a "~~~" line
				// inside a "~~~~"-opened block) is just content.
				fenceChar, fenceLen = 0, 0
			}
			continue
		}
		if fenceChar != 0 {
			continue
		}

		// Indented code is a *block*, not a per-line property: once a
		// blank line followed by a 4-space (or tab) indented, unmarked
		// line opens one, every subsequent indented line belongs to it —
		// regardless of whether that later line's own predecessor was
		// blank — until a non-blank, non-indented line closes it. Tracked
		// as running state across every line (URL or not), symmetric with
		// the fence tracking above, so a second URL deeper in the same
		// block doesn't fall through the guard the first URL was caught by.
		indent := leadingIndent(line)
		indented := hasCodeIndent(indent)
		switch {
		case inIndentedCode:
			if !blank && !indented {
				inIndentedCode = false
			}
		case wasPrevBlank && !blank && indented && !listMarkerPrefix.MatchString(line):
			inIndentedCode = true
		}
		if inIndentedCode {
			continue
		}

		m := bareURLLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		// Known boundary, not a bug: a *loose* list's second paragraph
		// (blank line, then a 4-space-indented URL under a bullet) opens
		// this same indented-code state and falls through to the old
		// wrap-split. Distinguishing it needs real list-context tracking;
		// the board convention doesn't produce loose lists, so this hasn't
		// been worth the complexity.
		urls = append(urls, m[3])
		lines[i] = m[1] + m[2] + urlPlaceholder(len(urls)-1) + m[4]
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

// stripControlBytes removes ASCII control bytes and DEL from s. The stashed
// URL is never seen by glamour (it's replaced with a placeholder before
// rendering), so nothing else in the pipeline neutralizes an embedded ESC
// or BEL before it becomes on-screen display text — xansi.Truncate passes
// escapes through unconditionally (that's the whole premise this PR relies
// on for the hyperlink itself to survive truncation) and termenv only
// styles text, it doesn't sanitize it. oscTarget percent-encodes the same
// bytes for the link target, where they need to stay meaningful; the
// visible text just needs them gone, since a board line is written by an
// agent pasting arbitrary tool output and an ESC there would otherwise
// reach the terminal as a real escape sequence — a nested OSC 8 pointing
// somewhere the board never named, or worse.
func stripControlBytes(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 0x20 && c != 0x7f {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// restoreBareURLs swaps each placeholder back for its real URL, wrapped in
// an OSC 8 hyperlink and styled like glamour's own Link (colorLink,
// underlined). The visible text is elided to fit whatever width remains on
// its line — measured from the actual rendered prefix and suffix (bullet,
// indent, nesting, and anything glamour reflowed onto the same physical
// line after the URL, e.g. a soft-wrapped paragraph continuation), not
// guessed — but the hyperlink target always carries the full, untruncated
// URL, so the link opens the right place regardless of how much of it is
// shown. Each URL has its own index-keyed placeholder, so two URLs that
// happen to elide to identical visible text can never cross-link — there's
// nothing to search for and collide on. If there's no room at all (a
// pathologically narrow pane with a deep indent), the URL is dropped
// rather than drawn — an over-width line would break the one invariant
// every other line in this renderer holds.
func restoreBareURLs(rendered string, urls []string, width int) string {
	if len(urls) == 0 {
		return rendered
	}
	lines := strings.Split(rendered, "\n")
	lr, lg, lb := hexToRGB(colorLink) // loop-invariant
	for i, url := range urls {
		ph := urlPlaceholder(i)
		for li, line := range lines {
			idx := strings.Index(line, ph)
			if idx < 0 {
				continue
			}
			prefix := line[:idx]
			rest := line[idx+len(ph):]
			budget := width - visibleWidth(prefix) - visibleWidth(rest)
			if budget < 1 {
				lines[li] = prefix + rest
				break
			}
			// xansi.Truncate, not go-runewidth: it's grapheme-cluster aware
			// (a VS16 emoji presentation sequence is one cluster but two
			// runes) and it's the same measurement visibleWidth uses to
			// enforce the pane-width invariant. Measuring the budget with
			// one metric and building the display text with a different
			// one is exactly how that invariant would quietly break again.
			display := xansi.Truncate(stripControlBytes(url), budget, "…")
			// Raw ANSI, not termenv.String: termenv.String binds to
			// termenv's auto-detected package-global Output profile, which
			// this codebase deliberately overrides everywhere else — the
			// glamour renderer is forced to termenv.TrueColor below, and
			// diff.go writes 38;2;r;g;b by hand for the same reason
			// ("piped/degraded profiles were how glow washed out"). Under
			// a degraded profile termenv.String would silently drop the
			// styling and this would be the one link on screen that isn't
			// truecolor.
			styled := fmt.Sprintf("\x1b[4;38;2;%d;%d;%dm%s\x1b[0m", lr, lg, lb, display)
			lines[li] = prefix + hyperlink(url, styled) + rest
			break
		}
	}
	return strings.Join(lines, "\n")
}

// renderMarkdown renders raw markdown at the given width (already reduced
// from the pane width by the caller). Output is post-processed to guarantee
// the hard requirements: no trailing-space padding, at most one blank line
// between blocks, no leading/trailing blank runs.
//
// linkify controls the bare-URL OSC 8 hyperlink path: true for the
// interactive TUI, where the escape sequence is invisible to the terminal
// and only the (possibly elided) display text is shown. false skips
// stashBareURLs entirely, so bare URLs render as plain, complete,
// un-elided text — for a non-TTY consumer (runStatic piped to a file,
// grep, a CI log) whose stdout is not a terminal that would render the
// hyperlink at all; there, showing the full plain URL beats hiding it
// behind an escape sequence that reader can't see.
func renderMarkdown(raw string, width int, linkify bool) (string, error) {
	if width < 10 {
		width = 10
	}
	var urls []string
	if linkify {
		raw, urls = stashBareURLs(raw)
	}
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
	if len(urls) > 0 {
		out = restoreBareURLs(out, urls, width)
		// Defensive: if a placeholder somehow didn't survive glamour intact
		// (an unanticipated reflow edge case), strip the residual bytes
		// rather than let a raw \x1f-delimited token land on screen — the
		// URL is lost either way at that point; better a bare, mostly
		// inert failure than one carrying our own control bytes. The plain
		// \x1f pass after it also catches a placeholder that got hard-
		// broken across a wrap (the paired-delimiter regex above can't
		// match half a token) — \x1f itself is unambiguously our own byte
		// (nothing else in this pipeline emits it), so it's always safe to
		// drop. It's not a fully invisible failure in that split case,
		// though: only the delimiter bytes are removed, so a naked "U0"
		// can be left as plain visible text. Narrowing that further would
		// mean matching digits without a \x1f anchor, which risks eating
		// real board text (e.g. "U2", "Update") instead — worse than the
		// cosmetic residue it would prevent.
		out = residualPlaceholder.ReplaceAllString(out, "")
		out = strings.ReplaceAll(out, "\x1f", "")
	}
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
