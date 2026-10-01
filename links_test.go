package main

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

const linkBoard = `# Board

## 🧠 Needs you

- Fixed it, see https://example.com/pull/7/files for details.
  https://github.com/than/sidecar/pull/30
  Ask: Did the deploy behave?
  Next: review.
`

func linkModel(t *testing.T) (model, *[]string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, p, linkBoard)
	m := testModel(t, p)
	m.mouse = true
	var opened []string
	prev := openURL
	openURL = func(u string) error { opened = append(opened, u); return nil }
	t.Cleanup(func() { openURL = prev })
	return m, &opened
}

func TestLinkSpansFindLabelAndBareURLColumns(t *testing.T) {
	m, _ := linkModel(t)
	found := map[string]linkSpan{}
	for _, ln := range m.renderedLines {
		for _, s := range linkSpans(ln) {
			found[s.url] = s
			plain := stripANSI(ln)
			if s.from >= s.to || s.to > visibleWidth(plain)+1 {
				t.Fatalf("span %+v outside the line %q", s, plain)
			}
		}
	}
	if _, ok := found["https://example.com/pull/7/files"]; !ok {
		t.Fatalf("inline link not found: %v", found)
	}
	if _, ok := found["https://github.com/than/sidecar/pull/30"]; !ok {
		t.Fatalf("bare URL not found: %v", found)
	}
}

func TestLinkSpanColumnsCoverTheDisplayText(t *testing.T) {
	line := "ab " + hyperlink("https://x.test/a", "label") + " tail"
	spans := linkSpans(line)
	if len(spans) != 1 || spans[0].from != 3 || spans[0].to != 8 || spans[0].url != "https://x.test/a" {
		t.Fatalf("spans %+v", spans)
	}
	// ST-terminated links and styling sequences before the link count the same.
	st := "\x1b[31mab\x1b[0m \x1b]8;;https://y.test\x1b\\go\x1b]8;;\x1b\\"
	if s := linkSpans(st); len(s) != 1 || s[0].from != 3 || s[0].to != 5 || s[0].url != "https://y.test" {
		t.Fatalf("ST spans %+v", s)
	}
}

func TestClickingALinkOpensItInsteadOfTheItem(t *testing.T) {
	m, opened := linkModel(t)
	m = clickAt(t, m, "example.com/pull/7", 1)
	if len(*opened) != 1 || (*opened)[0] != "https://example.com/pull/7/files" {
		t.Fatalf("opened %v", *opened)
	}
	if !strings.HasPrefix(m.notice, "opened ") {
		t.Fatalf("notice %q", m.notice)
	}
	if m.itemSec != -1 {
		t.Fatal("a link click should not also select or tick the item")
	}
}

func TestClickingABareURLLineOpensIt(t *testing.T) {
	m, opened := linkModel(t)
	clickAt(t, m, "https://github.com/than", 3)
	if len(*opened) != 1 || (*opened)[0] != "https://github.com/than/sidecar/pull/30" {
		t.Fatalf("opened %v", *opened)
	}
}

func TestClickNextToALinkStillActsOnTheItem(t *testing.T) {
	m, opened := linkModel(t)
	m = clickAt(t, m, "• Fixed it", 0) // the bullet, on the same line as a link
	if len(*opened) != 0 || !strings.Contains(readFile(t, m.path), "[x] Fixed it") {
		t.Fatalf("bullet click should tick, opened %v", *opened)
	}
}

func TestOnlyWebAndMailLinksOpen(t *testing.T) {
	for u, want := range map[string]bool{
		"https://a.test": true, "http://a.test": true, "mailto:a@b.test": true,
		"file:///etc/passwd": false, "javascript:alert(1)": false, "vscode://x": false, "ssh://h": false,
	} {
		if linkAllowed(u) != want {
			t.Fatalf("linkAllowed(%q) = %v, want %v", u, !want, want)
		}
	}
	m, opened := linkModel(t)
	m.openLink("file:///etc/passwd")
	if len(*opened) != 0 || !strings.Contains(m.notice, "won't open") {
		t.Fatalf("opened %v, notice %q", *opened, m.notice)
	}
}

func TestHoveringALinkPaintsItAndShowsTheAddress(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	m, _ := linkModel(t)
	rest := m.vp.View()
	m = mouseAt(t, m, "example.com/pull/7", 1, tea.MouseActionMotion, tea.MouseButtonNone)
	if m.hover.kind != hoverLink || m.vp.View() == rest || !strings.Contains(m.vp.View(), linkHotStyle.Render("example.com/pull/7/files")) {
		t.Fatalf("hovering a link should paint it (hover %+v)", m.hover)
	}
	if bar := stripANSI(m.statusBar()); !strings.Contains(bar, "https://example.com/pull/7/files") {
		t.Fatalf("status bar should show the full address: %q", bar)
	}
	m = mouseAt(t, m, "Next: review", 0, tea.MouseActionMotion, tea.MouseButtonNone)
	if m.hover.line != -1 || m.vp.View() != rest {
		t.Fatal("moving off the link should restore it")
	}
}

func TestClicksAreHeldWhileTyping(t *testing.T) {
	m, opened := linkModel(t)
	m = press(t, m, "j", "a")
	if !m.typing {
		t.Fatal("setup: reply should be open")
	}
	m = clickAt(t, m, "example.com/pull/7", 1)
	if len(*opened) != 0 || !m.typing || !strings.Contains(m.notice, "finish the reply") {
		t.Fatalf("a click while typing must not act (opened %v, typing %v, notice %q)", *opened, m.typing, m.notice)
	}
}

func TestOKeyOpensTheSelectedItemsLinksOneByOne(t *testing.T) {
	m, opened := linkModel(t)
	m = press(t, m, "j")
	if !strings.Contains(m.hint(), "o open link") {
		t.Fatalf("the hint should offer o when the item has a link: %q", m.hint())
	}
	m = press(t, m, "o")
	m = press(t, m, "o")
	m = press(t, m, "o")
	want := []string{"https://example.com/pull/7/files", "https://github.com/than/sidecar/pull/30", "https://example.com/pull/7/files"}
	if strings.Join(*opened, " ") != strings.Join(want, " ") {
		t.Fatalf("opened %v, want %v", *opened, want)
	}
	if !strings.Contains(m.notice, "of 2") {
		t.Fatalf("notice %q should say which link", m.notice)
	}
}

func TestOKeyOnAnItemWithoutLinksSaysSo(t *testing.T) {
	m, _ := marksModel(t)
	opened := &[]string{}
	prev := openURL
	openURL = func(u string) error { *opened = append(*opened, u); return nil }
	t.Cleanup(func() { openURL = prev })
	m = press(t, m, "j", "o")
	if len(*opened) != 0 || m.notice != "no link on this item" {
		t.Fatalf("opened %v, notice %q", *opened, m.notice)
	}
}

func TestQuestionWithALinkInItStillWrapsAndColors(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sidecar.md")
	writeFile(t, p, "## 🧠 Needs you\n\n- Result.\n  Ask: Did https://example.com/some/long/path/here/for/wrapping work for you after the deploy finished?\n  Next: review.\n")
	m := testModel(t, p)
	found := false
	for _, ln := range m.renderedLines {
		if strings.HasPrefix(plainText(ln), "  ? Did") {
			found = true
		}
	}
	if !found {
		t.Fatalf("question with a link should still be drawn as an indented block:\n%s", stripANSI(strings.Join(m.renderedLines, "\n")))
	}
}
