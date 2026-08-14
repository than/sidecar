package main

import (
	"fmt"
	"regexp"
	"strconv"
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

// inlineURLFind matches a bare URL anywhere on a line, including mid-
// sentence. stashBareURLs only ever runs it on lines bareURLLine already
// rejected, so the two paths never see the same URL.
var inlineURLFind = regexp.MustCompile(`https?://\S+`)

// refDefLine matches a link reference definition ("[1]: https://…"), whose
// URL is markdown syntax rather than displayed text.
var refDefLine = regexp.MustCompile(`^[ \t]*\[[^\]]*\]:`)

// inlinePlaceholderFind matches a reserved-width inline placeholder,
// capturing its index and its padding run.
var inlinePlaceholderFind = regexp.MustCompile("\x1fI(\\d+)(x*)\x1f")

// trimURLTail moves trailing sentence punctuation back out of a URL. A
// whitespace-delimited match swallows the period ending the sentence, the
// comma before the next clause, and the paren that opened before the URL —
// none of which belong in the link target. A closing paren is only trimmed
// when the URL has no unmatched opening one, so a Wikipedia-style
// "…/Foo_(bar)" survives intact.
func trimURLTail(url string) string {
	for len(url) > 0 {
		c := url[len(url)-1]
		switch c {
		case '.', ',', ';', ':', '!', '?', '\'', '"':
			url = url[:len(url)-1]
			continue
		case ')':
			if strings.Count(url, "(") >= strings.Count(url, ")") {
				return url
			}
			url = url[:len(url)-1]
			continue
		}
		return url
	}
	return url
}

// inlineDisplay is the visible text for an inline URL: the scheme dropped,
// control bytes removed. Mid-sentence there's no room for "https://" and it
// carries no information a reader needs — the full URL stays in the OSC 8
// target either way.
func inlineDisplay(url string) string {
	u := stripControlBytes(url)
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	return u
}

// inlineReserve is how many cells an inline URL's display text gets. It is
// baked into the placeholder's own width so glamour wraps the sentence
// around a token of the final size, which is what keeps the URL off the
// wrap point entirely. Clamped below the wrap width because an oversize
// token doesn't split — it overflows, breaking the one invariant every
// line in this renderer holds.
func inlineReserve(display string, width int) int {
	max := width - 4
	if max < 8 {
		max = 8
	}
	if w := visibleWidth(display); w < max {
		return w
	}
	return max
}

// inlinePlaceholder builds a markdown-inert token exactly reserve cells
// wide: the \x1f delimiters measure zero, so "I", the index digits and the
// "x" padding carry the whole width. Padding with a letter (not "_", "*" or
// "~") keeps goldmark's inline parser from finding emphasis in it.
func inlinePlaceholder(idx, reserve int) string {
	body := "I" + strconv.Itoa(idx)
	if pad := reserve - len(body); pad > 0 {
		body += strings.Repeat("x", pad)
	}
	return "\x1f" + body + "\x1f"
}

// stashInlineURLs replaces each bare URL inside line with a reserved-width
// placeholder, appending the URLs to *urls. URLs that are already markdown
// syntax — the target of "[label](url)", an "<url>" autolink, or a link
// reference definition — are glamour's to render and are left alone.
func stashInlineURLs(line string, urls *[]string, width int) string {
	if refDefLine.MatchString(line) {
		return line
	}
	matches := inlineURLFind.FindAllStringIndex(line, -1)
	if matches == nil {
		return line
	}
	var b strings.Builder
	cursor := 0
	for _, m := range matches {
		start, end := m[0], m[1]
		if start >= 2 && line[start-2:start] == "](" {
			continue // the target half of an inline link
		}
		if start >= 1 && line[start-1] == '<' {
			continue // an autolink
		}
		url := trimURLTail(line[start:end])
		if url == "" {
			continue
		}
		b.WriteString(line[cursor:start])
		cursor = start + len(url)
		idx := len(*urls)
		*urls = append(*urls, url)
		b.WriteString(inlinePlaceholder(idx, inlineReserve(inlineDisplay(url), width)))
	}
	if cursor == 0 {
		return line
	}
	b.WriteString(line[cursor:])
	return b.String()
}

// restoreInlineURLs swaps every reserved-width placeholder for its styled,
// hyperlinked display text. The substitution is width-for-width — the
// placeholder was built at exactly the size the display text renders to —
// so no line changes width and the shared-budget arithmetic the own-line
// path does afterwards still measures a truthful line.
func restoreInlineURLs(rendered string, urls []string, lr, lg, lb int) string {
	return inlinePlaceholderFind.ReplaceAllStringFunc(rendered, func(tok string) string {
		m := inlinePlaceholderFind.FindStringSubmatch(tok)
		idx, err := strconv.Atoi(m[1])
		if err != nil || idx < 0 || idx >= len(urls) {
			return ""
		}
		reserve := len(m[1]) + len(m[2]) + 1 // "I" + digits + padding
		display := xansi.Truncate(inlineDisplay(urls[idx]), reserve, "…")
		styled := fmt.Sprintf("\x1b[4;38;2;%d;%d;%dm%s\x1b[0m", lr, lg, lb, display)
		return hyperlink(urls[idx], styled)
	})
}

// stashBareURLs replaces every bare-URL-only line with a short placeholder,
// returning the stashed URLs in order (skipping lines inside fenced or
// indented code blocks). Glamour's word-wrap hard-splits a long link token
// mid-URL once it exceeds the wrap width — it treats the autolink's
// rendered ANSI run differently from plain text and force-breaks it instead
// of pushing the whole word to the next line — so the placeholder keeps
// such lines out of that path entirely. restoreBareURLs puts the real,
// styled, hyperlinked URL back after rendering.
func stashBareURLs(raw string, width int) (string, []string) {
	// A board line already containing \x1f — a pasted-tool-output edge
	// case, the same threat model stripControlBytes exists for — would
	// otherwise prefix-match a real placeholder in restoreBareURLs' search
	// and substitute the wrong URL. \x1f has no legitimate use in board
	// text, so it's always safe to drop from the input outright.
	raw = strings.ReplaceAll(raw, "\x1f", "")
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

		if fenceChar != 0 {
			// Already inside a fence: only a run of the same character,
			// at least as long as the one that opened it, closes it —
			// anything else (including a run of the other fence
			// character) is just content.
			if m := fenceLine.FindStringSubmatch(line); m != nil {
				c, n := m[1][0], len(m[1])
				if c == fenceChar && n >= fenceLen {
					fenceChar, fenceLen = 0, 0
				}
			}
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
		if inIndentedCode {
			if blank || indented {
				continue
			}
			inIndentedCode = false
			// Falls through to the fence/open checks below rather than
			// an else-branch skip: the line that closes an indented code
			// block (non-blank, non-indented — the case that just fell
			// through above) still needs to be checked as a possible
			// fence opener itself. Missing that let a ``` line closing
			// an indented block skip fence-recognition entirely, so the
			// fence it should have opened never did, the bare URL right
			// after it went unprotected, and the *next* fence-looking
			// line (meant to close that fence) opened a phantom one
			// instead — silently reverting every real bare URL for the
			// rest of the document. `indented` is false here by
			// construction (that's what let this branch fall through at
			// all), so the indented-code-open check below cannot misfire
			// on this same line.
		}
		// A fence delimiter only opens a fence outside an indented code
		// block — a ``` or ~~~ line that's itself part of one is just
		// indented content, same as any other line in it, so this check
		// has to come after the indented-code state above or an indented
		// fence-looking line would open a fence that never finds its
		// close and silently reverts every bare URL for the rest of the
		// document.
		if m := fenceLine.FindStringSubmatch(line); m != nil {
			fenceChar, fenceLen = m[1][0], len(m[1])
			continue
		}
		if wasPrevBlank && !blank && indented && !listMarkerPrefix.MatchString(line) {
			inIndentedCode = true
			continue
		}

		m := bareURLLine.FindStringSubmatch(line)
		if m == nil {
			// Not a URL-only line, but it may still carry one inside a
			// sentence. Those fall through to glamour untouched otherwise:
			// never hyperlinked, and hard-split at the wrap point.
			lines[i] = stashInlineURLs(line, &urls, width)
			continue
		}
		// Known boundary, not a bug: a *loose* list's second paragraph
		// (blank line, then a 4-space-indented URL under a bullet) opens
		// this same indented-code state and falls through to the old
		// wrap-split. Distinguishing it needs real list-context tracking;
		// the board convention doesn't produce loose lists, so this hasn't
		// been worth the complexity.
		urls = append(urls, m[3])
		// m[4] (trailing whitespace) is dropped, not reinserted: tidy()
		// strips it from the final output anyway, and keeping it here
		// would shrink restoreBareURLs' elision budget for spaces nobody
		// sees.
		lines[i] = m[1] + m[2] + urlPlaceholder(len(urls)-1)
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

// placeholderFind locates every placeholder on a line at once, capturing
// its index — used by restoreBareURLs to size the shared budget when more
// than one bare URL reflowed onto the same physical line.
var placeholderFind = regexp.MustCompile(`\x1fU(\d+)\x1f`)

// restoreBareURLs swaps each placeholder back for its real URL, wrapped in
// an OSC 8 hyperlink and styled like glamour's own Link (colorLink,
// underlined). The unit of work is the rendered LINE, not the individual
// URL: every placeholder on a line shares one width budget, computed once
// from the line with all placeholders removed and then split evenly across
// however many there are. Processing URLs independently (each measuring
// its budget against a line that already had earlier URLs' full-size
// hyperlinks substituted in) let the first URL on a shared line claim
// nearly the whole width and starve the rest down to a cell or two — width
// invariant intact, but a real bug: two bare URLs as consecutive
// continuation lines of one board item reflow onto a single physical line
// exactly like this. The hyperlink target always carries the full,
// untruncated URL regardless of what its display text elides to. Each URL
// has its own index-keyed placeholder, so two URLs that happen to elide to
// identical visible text can never cross-link — there's nothing to search
// for and collide on. If a URL's share of the line's budget is under 1
// cell, it's dropped rather than drawn — an over-width line would break
// the one invariant every other line in this renderer holds.
func restoreBareURLs(rendered string, urls []string, width int) string {
	if len(urls) == 0 {
		return rendered
	}
	lr, lg, lb := hexToRGB(colorLink) // loop-invariant
	// Inline URLs first, and width-for-width: their placeholders already
	// reserved exactly the cells their display text occupies. Doing them
	// first means the own-line budget arithmetic below measures a line whose
	// inline links are real content at their real width, which is what it
	// would have measured had they never been stashed.
	rendered = restoreInlineURLs(rendered, urls, lr, lg, lb)
	lines := strings.Split(rendered, "\n")
	for li, line := range lines {
		matches := placeholderFind.FindAllStringSubmatchIndex(line, -1)
		if matches == nil {
			continue
		}

		// glamour's MarginWriter pads every short block line with
		// trailing spaces out to the full block width (see styleConfig's
		// doc comment). Real trailing content on the line only ever needs
		// the width AFTER that padding, never the padding itself — left
		// uncorrected, the padding reads as content already filling the
		// line and starves the elision budget to almost nothing,
		// regardless of how much real room there actually is. The
		// hyperlink target and the width invariant both stay technically
		// correct at any budget, including a useless one, so this needs
		// its own check rather than relying on those. Strip the padding
		// by measuring the plain-text line with real trailing spaces
		// trimmed, then cutting the ANSI-styled line to that same visible
		// width — x/ansi.Truncate is escape-aware, so this keeps every
		// SGR code and every placeholder intact and only drops the
		// trailing filler.
		realWidth := visibleWidth(strings.TrimRight(stripANSI(line), " "))
		line = xansi.Truncate(line, realWidth, "")
		matches = placeholderFind.FindAllStringSubmatchIndex(line, -1)
		if matches == nil {
			continue // placeholders are never whitespace; stay safe anyway
		}

		available := width - visibleWidth(placeholderFind.ReplaceAllString(line, ""))
		share := available / len(matches)

		var out strings.Builder
		cursor := 0
		for _, m := range matches {
			start, end := m[0], m[1]
			out.WriteString(line[cursor:start])
			cursor = end
			idx, err := strconv.Atoi(line[m[2]:m[3]])
			if err != nil || idx < 0 || idx >= len(urls) || share < 1 {
				continue // drop this URL: no room, or a malformed index
			}
			url := urls[idx]
			// xansi.Truncate, not go-runewidth: it's grapheme-cluster
			// aware (a VS16 emoji presentation sequence is one cluster
			// but two runes) and it's the same measurement visibleWidth
			// uses to enforce the pane-width invariant. Measuring the
			// budget with one metric and building the display text with
			// a different one is exactly how that invariant would
			// quietly break again.
			display := xansi.Truncate(stripControlBytes(url), share, "…")
			// Raw ANSI, not termenv.String: termenv.String binds to
			// termenv's auto-detected package-global Output profile,
			// which this codebase deliberately overrides everywhere else
			// — the glamour renderer is forced to termenv.TrueColor
			// below, and diff.go writes 38;2;r;g;b by hand for the same
			// reason ("piped/degraded profiles were how glow washed
			// out"). Under a degraded profile termenv.String would
			// silently drop the styling and this would be the one link
			// on screen that isn't truecolor.
			styled := fmt.Sprintf("\x1b[4;38;2;%d;%d;%dm%s\x1b[0m", lr, lg, lb, display)
			out.WriteString(hyperlink(url, styled))
		}
		out.WriteString(line[cursor:])
		lines[li] = out.String()
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
// stashBareURLs entirely — for a non-TTY consumer (runStatic piped to a
// file, grep, a CI log) whose stdout is not a terminal that would render
// the hyperlink at all, an OSC 8 escape it can't see is worse than plain
// text. It isn't a full guarantee, though: a URL long enough to still hit
// glamour's own hard word-wrap on that path renders un-elided but still
// split mid-URL, same as before this fix — this only helps the common
// case, a URL short enough to need no wrap at the non-TTY fallback width.
func renderMarkdown(raw string, width int, linkify bool) (string, error) {
	if width < 10 {
		width = 10
	}
	var urls []string
	if linkify {
		raw, urls = stashBareURLs(raw, width)
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
