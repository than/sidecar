package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const editBoard = `# Board

## 🧠 Needs you

- Ship it
- Which name?
  Ask: What should we call it?
  Next: answer

## 🚧 In progress

- ` + emptySectionPlaceholder + `

## ✅ Done

- old
`

func writeBoardFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sidecar.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	d, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(d)
}

func TestToggleTickBothWays(t *testing.T) {
	on, err := toggleTick([]string{"- plain", "  more"})
	if err != nil || on[0] != "- [x] plain" || on[1] != "  more" {
		t.Fatalf("tick: %v %v", on, err)
	}
	off, _ := toggleTick([]string{"- [X] plain"})
	if off[0] != "- plain" {
		t.Fatalf("untick returns to a plain bullet: %v", off)
	}
	box, _ := toggleTick([]string{"- [ ] task"})
	if box[0] != "- [x] task" {
		t.Fatalf("an unchecked box ticks: %v", box)
	}
	if _, err := toggleTick([]string{"1. numbered"}); err == nil {
		t.Fatal("only dash bullets tick")
	}
}

func TestSetAnswerGoesUnderAskAndReplaces(t *testing.T) {
	b, _ := parseBoard(editBoard)
	lines := strings.Split(b.Sections[0].Items[1].Raw, "\n")
	a, err := setAnswer(lines, "Sidecar")
	if err != nil {
		t.Fatal(err)
	}
	if a[2] != "  Answer: Sidecar" || len(a) != 4 || a[3] != "  Next: answer" {
		t.Fatalf("placement: %q", a)
	}
	a2, _ := setAnswer(a, "Wingman")
	if len(a2) != 4 || a2[2] != "  Answer: Wingman" {
		t.Fatalf("a new reply must replace the old one: %q", a2)
	}
}

func TestSetAnswerOnAnItemThatAskedNothingGoesLast(t *testing.T) {
	a, _ := setAnswer([]string{"- Create the key", "  Next: create it"}, "made it")
	if strings.Join(a, "|") != "- Create the key|  Next: create it|  Answer: made it" {
		t.Fatalf("%q", a)
	}
}

func TestEditItemWritesOnlyThatItem(t *testing.T) {
	p := writeBoardFile(t, editBoard)
	b, _ := parseBoard(editBoard)
	if err := editItem(p, "🧠 Needs you", b.Sections[0].Items[0].Raw, replaceLines(toggleTick)); err != nil {
		t.Fatal(err)
	}
	if got, want := readFile(t, p), strings.Replace(editBoard, "- Ship it", "- [x] Ship it", 1); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEditItemRefusesWhenItemChanged(t *testing.T) {
	p := writeBoardFile(t, strings.Replace(editBoard, "Ship it", "Ship it now", 1))
	b, _ := parseBoard(editBoard)
	if err := editItem(p, "🧠 Needs you", b.Sections[0].Items[0].Raw, replaceLines(toggleTick)); err != errBoardChanged {
		t.Fatalf("want errBoardChanged, got %v", err)
	}
}

func TestEditItemKeepsConcurrentEditsElsewhere(t *testing.T) {
	p := writeBoardFile(t, editBoard+"- added by agent\n")
	b, _ := parseBoard(editBoard)
	if err := editItem(p, "🧠 Needs you", b.Sections[0].Items[0].Raw, replaceLines(toggleTick)); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, p); !strings.Contains(got, "- added by agent") || !strings.Contains(got, "- [x] Ship it") {
		t.Fatalf("lost an edit:\n%s", got)
	}
}

func TestDiffNamesTickAndReply(t *testing.T) {
	old := "## 🧠 Needs you\n\n- Ship it\n- Pick\n  Ask: which?\n"
	next := "## 🧠 Needs you\n\n- [x] Ship it\n- Pick\n  Ask: which?\n  Answer: the first\n"
	ob, _ := parseBoard(old)
	nb, _ := parseBoard(next)
	got := strings.Join(semanticDiff(ob, nb), "\n")
	for _, want := range []string{`edited 🧠: "Ship it" — ticked`, `edited 🧠: "Pick" — replied "the first"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	back, _ := parseBoard(old)
	if got := strings.Join(semanticDiff(nb, back), "\n"); !strings.Contains(got, `"Ship it" — unticked`) {
		t.Fatalf("untick to a plain bullet must still be reported:\n%s", got)
	}
}
