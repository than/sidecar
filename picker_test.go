// picker_test.go
package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func send(p picker, keys ...string) picker {
	for _, k := range keys {
		next, _ := p.Update(key(k))
		p = next.(picker)
	}
	return p
}

func TestPickerToggleExcludes(t *testing.T) {
	p := send(newPicker(defaultSections()), "space") // deselect row 0 (Needs action)
	got := p.result()
	if len(got) != 4 || got[0].Name != "In progress" {
		t.Fatalf("toggle didn't exclude row 0: %+v", got)
	}
}

func TestPickerReorderDown(t *testing.T) {
	p := send(newPicker(defaultSections()), "J") // move row 0 down past row 1
	got := p.result()
	if got[0].Name != "In progress" || got[1].Name != "Needs action" {
		t.Fatalf("J did not reorder: %+v", got[:2])
	}
	if p.cursor != 1 {
		t.Errorf("cursor should follow moved row, got %d", p.cursor)
	}
}

func TestPickerReorderBounds(t *testing.T) {
	p := send(newPicker(defaultSections()), "K") // already at top; no-op
	if p.result()[0].Name != "Needs action" {
		t.Errorf("K at top should be a no-op")
	}
}

func TestPickerDelete(t *testing.T) {
	p := send(newPicker(defaultSections()), "d")
	if len(p.result()) != 4 || p.result()[0].Name != "In progress" {
		t.Fatalf("delete row 0 failed: %+v", p.result())
	}
}

func TestPickerAddAndEdit(t *testing.T) {
	// a: insert blank after cursor and enter edit (emoji field).
	// type "★", enter -> name field, type "Blocked", enter -> hint field,
	// type "waiting", enter -> commit.
	p := newPicker(defaultSections())
	p = send(p, "a", "★", "enter", "Blocked", "enter", "waiting", "enter")
	got := p.result()
	var found *Section
	for i := range got {
		if got[i].Name == "Blocked" {
			found = &got[i]
		}
	}
	if found == nil {
		t.Fatalf("added section not present: %+v", got)
	}
	if found.Emoji != "★" || found.Hint != "waiting" {
		t.Errorf("added section fields wrong: %+v", *found)
	}
}

func TestPickerAddEmptyNameDropped(t *testing.T) {
	// a: add, then leave name empty -> row is dropped on commit.
	p := newPicker(defaultSections())
	p = send(p, "a", "enter", "enter", "enter") // empty emoji, empty name, empty hint
	if len(p.result()) != 5 {
		t.Errorf("empty-name add should be dropped, got %d rows", len(p.result()))
	}
}

func TestPickerEditExisting(t *testing.T) {
	// e on row 0: keep emoji (enter), rename to "Inbox" (enter), keep hint.
	p := newPicker(defaultSections())
	p = send(p, "e", "enter", "Inbox", "enter", "enter")
	if p.result()[0].Name != "Inbox" {
		t.Errorf("edit didn't rename row 0: %+v", p.result()[0])
	}
}

func TestPickerCancelReturnsNil(t *testing.T) {
	p := send(newPicker(defaultSections()), "space", "esc")
	if p.result() != nil {
		t.Errorf("cancel should return nil, got %+v", p.result())
	}
}

func TestPickerAcceptQuits(t *testing.T) {
	next, cmd := newPicker(defaultSections()).Update(key("enter"))
	if cmd == nil {
		t.Error("enter should return a quit command")
	}
	if !next.(picker).done {
		t.Error("enter should mark the picker done")
	}
}
