// inline_url_test.go — a URL inside a sentence, rather than alone on its
// line, used to fall straight through to glamour: never hyperlinked, and
// hard-split mid-URL at the wrap point ("https://github." / "com/o/r/pull/1").
package main

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
)

var testANSI = regexp.MustCompile("\x1b\\[[0-9;]*m")

// visibleText strips SGR styling and OSC 8 wrappers, leaving what a human
// reads on screen.
func visibleText(s string) string {
	s = regexp.MustCompile("\x1b\\]8;;[^\x07]*\x07").ReplaceAllString(s, "")
	return testANSI.ReplaceAllString(s, "")
}

// oscTargets returns every OSC 8 link target in s, in order.
func oscTargets(s string) []string {
	var out []string
	for _, m := range regexp.MustCompile("\x1b\\]8;;([^\x07]+)\x07").FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

const inlineSrc = "## S\n\n1. **Merge PR #301** — Review round done, 1349 PHP green. Closes #299. CI re-running after a flaky test (not mine — filed as #303). https://github.com/o/r/pull/301 *After merge, not before:* delete stuff.\n"

// The screenshot case: the URL must survive as one unbroken run, at every
// width, and must carry a working link target.
func TestInlineURLNeverSplitAcrossLines(t *testing.T) {
	for _, w := range []int{30, 40, 72, 100} {
		out, err := renderMarkdown(inlineSrc, w, true)
		if err != nil {
			t.Fatalf("width %d: %v", w, err)
		}
		if got := oscTargets(out); len(got) != 1 || got[0] != "https://github.com/o/r/pull/301" {
			t.Errorf("width %d: link targets = %v, want the one full URL", w, got)
		}
		// The visible display text must sit entirely on one rendered line.
		var found bool
		for _, ln := range strings.Split(visibleText(out), "\n") {
			if strings.Contains(ln, "github.com") {
				found = true
				if strings.HasSuffix(strings.TrimRight(ln, " "), "github.") {
					t.Errorf("width %d: URL split mid-host:\n%s", w, ln)
				}
			}
		}
		if !found {
			t.Errorf("width %d: URL display text vanished:\n%s", w, visibleText(out))
		}
	}
}

// Every rendered line stays inside the pane. A reserved-width placeholder
// wider than the wrap width would overflow instead of splitting (glamour
// force-breaks its own styled autolink runs, but pushes a plain token whole
// and lets it hang past the margin), so the reserve has to be clamped.
//
// Scope: this asserts the URL path only. Glamour's own wrapping can exceed
// the requested width by a cell on some inputs — an em-dash-heavy ordered
// list item does it on main with no URL present at all — which is a
// separate, pre-existing accounting issue and not what this guards.
func TestInlineURLKeepsWidthInvariant(t *testing.T) {
	long := "## S\n\n- before " + "https://example.test/" + strings.Repeat("segment/", 12) + "end after\n"
	for _, w := range []int{20, 30, 40, 72} {
		out, err := renderMarkdown(long, w, true)
		if err != nil {
			t.Fatalf("width %d: %v", w, err)
		}
		for _, ln := range strings.Split(out, "\n") {
			if got := xansi.StringWidth(ln); got > w {
				t.Errorf("width %d: line overflows pane at %d cells:\n%q", w, got, visibleText(ln))
			}
		}
		assertLinkSurvived(t, out, fmt.Sprintf("width %d", w))
	}
}

// A width assertion alone passes vacuously if the placeholder was broken and
// swept away: the URL is gone, so of course the line fits. Every width case
// has to prove the link came back — and that no padded residue reached the
// screen, which renderMarkdown's own comment calls the louder failure.
func assertLinkSurvived(t *testing.T, out, ctx string) {
	t.Helper()
	if len(oscTargets(out)) == 0 {
		t.Errorf("%s: URL vanished instead of rendering as a link:\n%s", ctx, visibleText(out))
	}
	if m := residueFind.FindString(visibleText(out)); m != "" {
		t.Errorf("%s: placeholder residue %q reached the screen:\n%s", ctx, m, visibleText(out))
	}
}

var residueFind = regexp.MustCompile(`I\d+x{2,}`)

// A URL glued to a prefix is one wrap word, so the affix has to come out of
// the reserve too — otherwise "Source:https://…" pushes the token past the
// clamp and the line hangs over the pane edge.
func TestInlineURLGluedAffixKeepsWidth(t *testing.T) {
	long := "https://example.test/" + strings.Repeat("segment/", 6) + "end"
	cases := map[string]string{
		"glued prefix":      "## S\n\n- before Source:" + long + " after\n",
		"long glued prefix": "## S\n\n- before PR-301-reference:" + long + " after\n",
		"glued suffix":      "## S\n\n- before " + long + "|trailing after\n",
	}
	for _, w := range []int{20, 30, 40, 72} {
		for name, src := range cases {
			out, err := renderMarkdown(src, w, true)
			if err != nil {
				t.Fatal(err)
			}
			for _, ln := range strings.Split(out, "\n") {
				if got := xansi.StringWidth(ln); got > w {
					t.Errorf("%s at width %d: overflows at %d cells:\n%q", name, w, got, visibleText(ln))
				}
			}
			assertLinkSurvived(t, out, fmt.Sprintf("%s at width %d", name, w))
		}
	}
}

// Trailing sentence punctuation belongs to the sentence, not the URL.
func TestInlineURLTrailingPunctuationNotInTarget(t *testing.T) {
	cases := map[string]string{
		"period":      "- see https://example.test/a. Next thing.\n",
		"comma":       "- see https://example.test/a, then more.\n",
		"paren":       "- see (https://example.test/a) and more.\n",
		"exclamation": "- see https://example.test/a! More.\n",
	}
	for name, src := range cases {
		out, err := renderMarkdown("## S\n\n"+src, 100, true)
		if err != nil {
			t.Fatal(err)
		}
		got := oscTargets(out)
		if len(got) != 1 || got[0] != "https://example.test/a" {
			t.Errorf("%s: targets = %v, want [https://example.test/a]", name, got)
		}
	}
}

// Markdown that already expresses a link must be left to glamour.
func TestInlineURLLeavesMarkdownLinkSyntaxAlone(t *testing.T) {
	for name, src := range map[string]string{
		"inline link": "- see [the PR](https://example.test/a) now.\n",
		"autolink":    "- see <https://example.test/a> now.\n",
		"ref def":     "- see [the PR][1] now.\n\n[1]: https://example.test/a\n",
	} {
		out, err := renderMarkdown("## S\n\n"+src, 100, true)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "\x1f") {
			t.Errorf("%s: a placeholder leaked into the output:\n%q", name, out)
		}
	}
}

// A URL inside a fence is verbatim content.
func TestInlineURLInFenceUntouched(t *testing.T) {
	src := "## S\n\n- item\n\n```\ncurl https://example.test/a && echo done\n```\n"
	out, err := renderMarkdown(src, 100, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(oscTargets(out)) != 0 {
		t.Errorf("fenced URL was hyperlinked:\n%q", out)
	}
	if !strings.Contains(visibleText(out), "https://example.test/a") {
		t.Errorf("fenced URL lost its scheme:\n%s", visibleText(out))
	}
}

// Two inline URLs in one sentence each get their own link.
func TestInlineURLTwoOnOneLine(t *testing.T) {
	src := "## S\n\n- compare https://example.test/aaa with https://example.test/bbb today.\n"
	out, err := renderMarkdown(src, 100, true)
	if err != nil {
		t.Fatal(err)
	}
	got := oscTargets(out)
	if len(got) != 2 || got[0] != "https://example.test/aaa" || got[1] != "https://example.test/bbb" {
		t.Errorf("targets = %v, want both URLs in order", got)
	}
}

// Same threat model as the own-line path: an agent pastes tool output with a
// live escape in it. The display text must not carry it; the target encodes it.
func TestInlineURLStripsControlBytesFromDisplay(t *testing.T) {
	src := "## S\n\n- see https://example.test/a\x1b]8;;evil\x07b now.\n"
	out, err := renderMarkdown(src, 100, true)
	if err != nil {
		t.Fatal(err)
	}
	body := out
	if i := strings.Index(body, "\x1b]8;;"); i >= 0 {
		body = body[i+len("\x1b]8;;"):]
	}
	if strings.Contains(visibleText(out), "\x1b") {
		t.Errorf("escape survived into display text:\n%q", out)
	}
	for _, tgt := range oscTargets(out) {
		if strings.ContainsAny(tgt, "\x1b\x07") {
			t.Errorf("raw escape in link target: %q", tgt)
		}
	}
}

// linkify=false is the non-TTY path and keeps its plain-text contract.
func TestInlineURLSkippedWhenNotLinkifying(t *testing.T) {
	out, err := renderMarkdown(inlineSrc, 72, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(oscTargets(out)) != 0 {
		t.Errorf("hyperlink emitted on the non-TTY path:\n%q", out)
	}
	if strings.Contains(out, "\x1f") {
		t.Errorf("placeholder leaked on the non-TTY path:\n%q", out)
	}
}

// The own-line path is unchanged: full URL as display text, one link.
func TestOwnLineURLStillFullDisplay(t *testing.T) {
	src := "## S\n\n- item\n  https://example.test/a\n"
	out, err := renderMarkdown(src, 72, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := oscTargets(out); len(got) != 1 || got[0] != "https://example.test/a" {
		t.Errorf("targets = %v", got)
	}
	if !strings.Contains(visibleText(out), "https://example.test/a") {
		t.Errorf("own-line URL should keep its scheme:\n%s", visibleText(out))
	}
}

// A backtick code span is a command the reader copies. Rewriting the URL
// inside one as scheme-stripped link-coloured text breaks the copy, and the
// styled replacement's reset terminates the span's own styling partway.
func TestInlineURLInCodeSpanUntouched(t *testing.T) {
	for name, src := range map[string]string{
		"single tick": "- run `curl https://example.test/a` and check\n",
		"double tick": "- run ``curl https://example.test/a`` and check\n",
		"two spans":   "- `https://example.test/a` then `https://example.test/b`\n",
	} {
		out, err := renderMarkdown("## S\n\n"+src, 100, true)
		if err != nil {
			t.Fatal(err)
		}
		if got := oscTargets(out); len(got) != 0 {
			t.Errorf("%s: URL in a code span was hyperlinked: %v", name, got)
		}
		if !strings.Contains(visibleText(out), "https://example.test/a") {
			t.Errorf("%s: code span lost the literal URL:\n%s", name, visibleText(out))
		}
	}
	// Text outside the span still gets linked.
	out, _ := renderMarkdown("## S\n\n- `curl https://example.test/a` see https://example.test/b\n", 100, true)
	if got := oscTargets(out); len(got) != 1 || got[0] != "https://example.test/b" {
		t.Errorf("targets = %v, want only the URL outside the span", got)
	}
}

// Emphasis markers wrap the URL; they are not part of it.
func TestInlineURLTrimsEmphasisMarkers(t *testing.T) {
	for name, src := range map[string]string{
		"italic": "- see *https://example.test/a* now.\n",
		"bold":   "- see **https://example.test/a** now.\n",
		"strike": "- see ~~https://example.test/a~~ now.\n",
	} {
		out, err := renderMarkdown("## S\n\n"+src, 100, true)
		if err != nil {
			t.Fatal(err)
		}
		got := oscTargets(out)
		if len(got) != 1 || got[0] != "https://example.test/a" {
			t.Errorf("%s: targets = %v, want the URL without its emphasis markers", name, got)
		}
	}
}

// If an inline placeholder ever survives restore, the blanket \x1f strip
// leaves its whole padded body on screen — up to a pane's width of "I7xxxx…".
// The residual sweep has to know the inline shape too.
func TestResidualSweepCoversInlinePlaceholder(t *testing.T) {
	for _, tok := range []string{"\x1fU3\x1f", "\x1fI7xxxxxxxxxxxx\x1f", "\x1fI0\x1f"} {
		if !residualPlaceholder.MatchString(tok) {
			t.Errorf("residual sweep misses %q", strings.ReplaceAll(tok, "\x1f", "|"))
		}
	}
	if residualPlaceholder.MatchString("\x1fnot a token\x1f") {
		t.Error("residual sweep matches arbitrary text")
	}
}

// The reserve clamp has to hold inside indented blocks, where glamour gives
// the content less room than the global wrap width.
func TestInlineURLWidthInvariantWhenIndented(t *testing.T) {
	long := "https://example.test/" + strings.Repeat("segment/", 12) + "end"
	cases := map[string]string{
		"nested x3":  "## S\n\n- a\n  - b\n    - before " + long + " after\n",
		"nested x5":  "## S\n\n- a\n  - b\n    - c\n      - d\n        - before " + long + " after\n",
		"blockquote": "## S\n\n> before " + long + " after\n",
		"bq nested":  "## S\n\n> > before " + long + " after\n",
	}
	// 10 is renderMarkdown's floor, and inlineReserve's own 8-cell floor
	// ignores block indent — the narrow rows prove that floor can't push a
	// token past the pane inside an indented block.
	for _, w := range []int{10, 12, 16, 20, 40, 72} {
		for name, src := range cases {
			out, err := renderMarkdown(src, w, true)
			if err != nil {
				t.Fatal(err)
			}
			for _, ln := range strings.Split(out, "\n") {
				if got := xansi.StringWidth(ln); got > w {
					t.Errorf("%s at width %d: line overflows at %d cells:\n%q", name, w, got, visibleText(ln))
				}
			}
			assertLinkSurvived(t, out, fmt.Sprintf("%s at width %d", name, w))
		}
	}
}

// Both kinds of URL in one item: the inline one is stashed with a padded
// placeholder, the continuation line with the own-line one, and they share
// an index space and a restore pass. Each must come back with its own intact
// target, and neither may starve the other's display text.
//
// What this does NOT cover, having tried: the two landing on the same
// rendered line. glamour keeps a bare-URL continuation line on its own line
// here — so do two consecutive own-line URLs, contrary to the reflow case
// restoreBareURLs' comment describes. The shared-budget arithmetic is
// defensive rather than live, so the ordering claim (inline restored first,
// width-for-width, leaving the budget maths a truthful line) can't be
// exercised end to end; it holds by construction instead.
func TestInlineAndOwnLineURLInOneItem(t *testing.T) {
	src := "## S\n\n- see https://example.test/inline for context\n  https://example.test/ownline\n"
	for _, w := range []int{60, 72, 100} {
		out, err := renderMarkdown(src, w, true)
		if err != nil {
			t.Fatalf("width %d: %v", w, err)
		}
		got := oscTargets(out)
		if len(got) != 2 {
			t.Errorf("width %d: got %d link targets, want 2: %v", w, len(got), got)
			continue
		}
		if got[0] != "https://example.test/inline" || got[1] != "https://example.test/ownline" {
			t.Errorf("width %d: targets = %v, want inline then own-line", w, got)
		}
		// Neither URL may starve the other, and the line must still fit.
		for _, ln := range strings.Split(out, "\n") {
			if x := xansi.StringWidth(ln); x > w {
				t.Errorf("width %d: line overflows at %d cells:\n%q", w, x, visibleText(ln))
			}
		}
		vis := visibleText(out)
		if !strings.Contains(vis, "example.test/inline") {
			t.Errorf("width %d: inline URL starved to nothing:\n%s", w, vis)
		}
		if !strings.Contains(vis, "example.test/ownline") {
			t.Errorf("width %d: own-line URL starved to nothing:\n%s", w, vis)
		}
	}
}

// An unclosed backtick run is literal text, but scanning resumes after it —
// a properly closed pair later on the same line is still a code span.
func TestCodeSpanScanResumesAfterUnclosedRun(t *testing.T) {
	src := "## S\n\n- a ``x and `curl https://example.test/a` done\n"
	out, err := renderMarkdown(src, 100, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := oscTargets(out); len(got) != 0 {
		t.Errorf("URL in a closed span after an unclosed run was hyperlinked: %v", got)
	}
	// Direct unit check on the scanner.
	line := "a ``x and `curl https://example.test/a` done"
	spans := codeSpanRanges(line)
	if len(spans) != 1 {
		t.Fatalf("codeSpanRanges = %v, want one span for the closed pair", spans)
	}
	if !strings.Contains(line[spans[0][0]:spans[0][1]], "curl") {
		t.Errorf("span %v is not the closed pair: %q", spans[0], line[spans[0][0]:spans[0][1]])
	}
}

// A URL in a link's label position is markdown syntax too. Stashing it made
// the click open the label URL rather than the real target.
func TestInlineURLAsLinkLabelLeftAlone(t *testing.T) {
	src := "## S\n\n- [https://example.test/label](https://example.test/target)\n"
	out, err := renderMarkdown(src, 100, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\x1f") {
		t.Errorf("a placeholder leaked:\n%q", out)
	}
	for _, tgt := range oscTargets(out) {
		if strings.Contains(tgt, "/label") {
			t.Errorf("click would open the label URL, not the target: %v", oscTargets(out))
		}
	}
}

// A scheme with nothing after it has no display text; emitting an OSC 8 pair
// around nothing is worse than leaving the text alone.
func TestInlineURLDegenerateSchemeOnly(t *testing.T) {
	// "https://." matches, then trimURLTail sheds the period and leaves a
	// bare scheme whose display text is empty.
	out, err := renderMarkdown("## S\n\n- see https://. now.\n", 100, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := oscTargets(out); len(got) != 0 {
		t.Errorf("empty hyperlink emitted: %v", got)
	}
	if strings.Contains(out, "\x1f") {
		t.Errorf("a placeholder leaked:\n%q", out)
	}
}

// Closing an inline link must restore the foreground glamour had set, not the
// terminal default — otherwise the rest of the run (body text, an H2's amber,
// an H1's black-on-lavender) renders in the wrong colour.
func TestInlineURLRestoresEnclosingForeground(t *testing.T) {
	out, err := renderMarkdown("## S\n\n- before https://example.test/a and after\n", 100, true)
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(out, oscClose)
	if i < 0 {
		t.Fatal("no hyperlink in output")
	}
	tail := out[i+len(oscClose):]
	if strings.HasPrefix(tail, "\x1b[24;39m") {
		t.Error("link close resets to the terminal default foreground instead of replaying the enclosing style")
	}
	// Whatever follows must re-establish an explicit colour before any text.
	if j := strings.IndexFunc(tail, func(r rune) bool { return r == 'a' }); j >= 0 {
		if !strings.Contains(tail[:j], "38;2;") {
			t.Errorf("no truecolor foreground re-established after the link: %q", tail[:min(j, 60)])
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
