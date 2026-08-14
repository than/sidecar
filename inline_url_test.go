// inline_url_test.go — a URL inside a sentence, rather than alone on its
// line, used to fall straight through to glamour: never hyperlinked, and
// hard-split mid-URL at the wrap point ("https://github." / "com/o/r/pull/1").
package main

import (
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
