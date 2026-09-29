package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const editBoard = `# Board

## 🧠 Needs you

- [ ] Ship it
- Pick a path
  Ask: yes | no | done
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

func TestToggleCheckboxBothWays(t *testing.T) {
	on, err := toggleCheckbox([]string{"- [ ] a", "  more"})
	if err != nil || on[0] != "- [x] a" || on[1] != "  more" {
		t.Fatalf("tick: %v %v", on, err)
	}
	off, _ := toggleCheckbox([]string{"- [X] a"})
	if off[0] != "- [ ] a" {
		t.Fatalf("untick: %v", off)
	}
	if _, err := toggleCheckbox([]string{"- plain"}); err == nil {
		t.Fatal("plain bullet must not toggle")
	}
}

func TestAskOptionsAndAnswer(t *testing.T) {
	b, _ := parseBoard(editBoard)
	it := b.Sections[0].Items[1]
	if got := strings.Join(askOptions(it), ","); got != "yes,no,done" {
		t.Fatalf("options %q", got)
	}
	lines := strings.Split(it.Raw, "\n")
	a, err := setAnswer(lines, "no")
	if err != nil {
		t.Fatal(err)
	}
	if a[2] != "  Answer: no" || len(a) != 4 {
		t.Fatalf("answer placement: %q", a)
	}
	a2, _ := setAnswer(a, "yes")
	if len(a2) != 4 || a2[2] != "  Answer: yes" {
		t.Fatalf("re-answer must replace: %q", a2)
	}
}

func TestEditItemWritesOnlyThatItem(t *testing.T) {
	p := writeBoardFile(t, editBoard)
	b, _ := parseBoard(editBoard)
	it := b.Sections[0].Items[0]
	if err := editItem(p, "🧠 Needs you", it.Raw, replaceLines(toggleCheckbox)); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(editBoard, "- [ ] Ship it", "- [x] Ship it", 1)
	if got := readFile(t, p); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestEditItemRefusesWhenItemChanged(t *testing.T) {
	p := writeBoardFile(t, strings.Replace(editBoard, "Ship it", "Ship it now", 1))
	b, _ := parseBoard(editBoard)
	err := editItem(p, "🧠 Needs you", b.Sections[0].Items[0].Raw, replaceLines(toggleCheckbox))
	if err != errBoardChanged {
		t.Fatalf("want errBoardChanged, got %v", err)
	}
}

func TestEditItemKeepsConcurrentEditsElsewhere(t *testing.T) {
	p := writeBoardFile(t, editBoard+"- added by agent\n")
	b, _ := parseBoard(editBoard)
	if err := editItem(p, "🧠 Needs you", b.Sections[0].Items[0].Raw, replaceLines(toggleCheckbox)); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, p); !strings.Contains(got, "- added by agent") || !strings.Contains(got, "- [x] Ship it") {
		t.Fatalf("lost an edit:\n%s", got)
	}
}

func TestMoveItemIntoPlaceholderSection(t *testing.T) {
	b, _ := parseBoard(editBoard)
	got, err := moveItem(editBoard, b, 0, 1, "🚧 In progress")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, emptySectionPlaceholder) {
		t.Fatalf("placeholder survived:\n%s", got)
	}
	b2, _ := parseBoard(got)
	if len(b2.Sections[0].Items) != 1 || !strings.HasPrefix(b2.Sections[1].Items[0].Raw, "- Pick a path\n  Ask:") {
		t.Fatalf("bad move:\n%s", got)
	}
}

func TestMoveItemAppendsAfterLastItem(t *testing.T) {
	b, _ := parseBoard(editBoard)
	got, err := moveItem(editBoard, b, 0, 0, "✅ Done")
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := parseBoard(got)
	done := b2.Sections[2].Items
	if len(done) != 2 || done[0].Key != "old" || done[1].Key != "Ship it" {
		t.Fatalf("bad move:\n%s", got)
	}
}

func TestMoveItemIntoTrulyEmptySection(t *testing.T) {
	raw := "## A\n\n- x\n\n## B\n\n## C\n\n- z\n"
	b, _ := parseBoard(raw)
	got, err := moveItem(raw, b, 0, 0, "B")
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := parseBoard(got)
	if len(b2.Sections[1].Items) != 1 || b2.Sections[1].Items[0].Key != "x" || len(b2.Sections[2].Items) != 1 {
		t.Fatalf("bad move:\n%s", got)
	}
}

func TestDiffNamesTickAndAnswer(t *testing.T) {
	old := "## 🧠 Needs you\n\n- [ ] Ship it\n- Pick\n  Ask: yes | no\n"
	next := "## 🧠 Needs you\n\n- [x] Ship it\n- Pick\n  Ask: yes | no\n  Answer: no\n"
	ob, _ := parseBoard(old)
	nb, _ := parseBoard(next)
	got := strings.Join(semanticDiff(ob, nb), "\n")
	for _, want := range []string{`edited 🧠: "Ship it" — ticked`, `edited 🧠: "Pick" — answered "no"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}
