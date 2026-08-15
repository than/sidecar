// md_link_test.go — a markdown link renders as its label, clickable, with
// the href hidden. glamour v1 has no OSC 8 at all and prints label AND href
// as plain text, so sidecar handles the construct itself.
package main

import (
	"strings"
	"testing"

	xansi "github.com/charmbracelet/x/ansi"
)

func TestMarkdownLinkRendersAsClickableLabel(t *testing.T) {
	src := "## S\n\n- Review round done. [the PR](https://github.com/o/r/pull/301) After merge, delete stuff.\n"
	for _, w := range []int{40, 72, 100} {
		out, err := renderMarkdown(src, w, true)
		if err != nil {
			t.Fatalf("width %d: %v", w, err)
		}
		got := oscTargets(out)
		if len(got) != 1 || got[0] != "https://github.com/o/r/pull/301" {
			t.Errorf("width %d: targets = %v, want the href", w, got)
		}
		vis := visibleText(out)
		if !strings.Contains(vis, "the PR") {
			t.Errorf("width %d: label missing:\n%s", w, vis)
		}
		if strings.Contains(vis, "github.com") {
			t.Errorf("width %d: href still visible, defeating the point:\n%s", w, vis)
		}
		if strings.Contains(vis, "[the PR]") || strings.Contains(vis, "](") {
			t.Errorf("width %d: raw markdown syntax leaked:\n%s", w, vis)
		}
		for _, ln := range strings.Split(out, "\n") {
			if x := xansi.StringWidth(ln); x > w {
				t.Errorf("width %d: line overflows at %d:\n%q", w, x, visibleText(ln))
			}
		}
	}
}

// The label is the wrap unit, so it must never be broken across lines.
func TestMarkdownLinkLabelNeverSplit(t *testing.T) {
	src := "## S\n\n- padding words here to force a wrap near the label [a longer label here](https://example.test/a) trailing text\n"
	for _, w := range []int{30, 36, 40, 44, 50} {
		out, err := renderMarkdown(src, w, true)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(visibleText(out), "\n")
		whole := false
		for _, ln := range lines {
			if strings.Contains(ln, "a longer label here") {
				whole = true
			}
		}
		if !whole && len(oscTargets(out)) > 0 {
			t.Errorf("width %d: label split across lines:\n%s", w, visibleText(out))
		}
	}
}

// A label that can't fit gets elided, exactly like a long URL does.
func TestMarkdownLinkLongLabelElides(t *testing.T) {
	src := "## S\n\n- [" + strings.Repeat("verylonglabel ", 6) + "](https://example.test/a) after\n"
	out, err := renderMarkdown(src, 40, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(oscTargets(out)) != 1 {
		t.Errorf("long label lost its link: %v", oscTargets(out))
	}
	for _, ln := range strings.Split(out, "\n") {
		if x := xansi.StringWidth(ln); x > 40 {
			t.Errorf("overflow at %d cells:\n%q", x, visibleText(ln))
		}
	}
}

// A non-URL target (relative path, anchor, mailto) stays glamour's problem.
func TestMarkdownLinkNonURLTargetUntouched(t *testing.T) {
	for name, src := range map[string]string{
		"relative": "- see [docs](./docs/readme.md) now\n",
		"anchor":   "- see [top](#heading) now\n",
		"mailto":   "- see [mail](mailto:a@b.test) now\n",
	} {
		out, err := renderMarkdown("## S\n\n"+src, 100, true)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "\x1f") {
			t.Errorf("%s: placeholder leaked:\n%q", name, out)
		}
	}
}

// Inside a code span the construct is literal text a reader copies.
func TestMarkdownLinkInCodeSpanUntouched(t *testing.T) {
	out, err := renderMarkdown("## S\n\n- run `[a](https://example.test/a)` now\n", 100, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(oscTargets(out)) != 0 {
		t.Errorf("code span was linkified: %v", oscTargets(out))
	}
	if !strings.Contains(visibleText(out), "[a](https://example.test/a)") {
		t.Errorf("code span content altered:\n%s", visibleText(out))
	}
}
