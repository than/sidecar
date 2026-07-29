// picker.go
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type editField int

const (
	fieldNone editField = iota
	fieldEmoji
	fieldName
	fieldHint
)

type pickerRow struct {
	Section
	included bool
}

type picker struct {
	rows     []pickerRow
	cursor   int
	editing  editField
	input    textinput.Model
	done     bool
	canceled bool
}

func newPicker(sections []Section) picker {
	rows := make([]pickerRow, len(sections))
	for i, s := range sections {
		rows[i] = pickerRow{Section: s, included: true}
	}
	return picker{rows: rows, input: textinput.New()}
}

func (p picker) Init() tea.Cmd { return nil }

func (p picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}
	if p.editing != fieldNone {
		return p.updateEditing(km)
	}
	return p.updateNav(km)
}

func (p picker) updateNav(km tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch km.String() {
	case "j", "down":
		if p.cursor < len(p.rows)-1 {
			p.cursor++
		}
	case "k", "up":
		if p.cursor > 0 {
			p.cursor--
		}
	case " ":
		if len(p.rows) > 0 {
			p.rows[p.cursor].included = !p.rows[p.cursor].included
		}
	case "J":
		if p.cursor < len(p.rows)-1 {
			p.rows[p.cursor], p.rows[p.cursor+1] = p.rows[p.cursor+1], p.rows[p.cursor]
			p.cursor++
		}
	case "K":
		if p.cursor > 0 {
			p.rows[p.cursor], p.rows[p.cursor-1] = p.rows[p.cursor-1], p.rows[p.cursor]
			p.cursor--
		}
	case "e":
		if len(p.rows) > 0 {
			p = p.beginEdit(fieldEmoji)
		}
	case "a":
		at := p.cursor + 1
		if len(p.rows) == 0 {
			at = 0
		}
		p.rows = append(p.rows, pickerRow{})
		copy(p.rows[at+1:], p.rows[at:])
		p.rows[at] = pickerRow{included: true}
		p.cursor = at
		p = p.beginEdit(fieldEmoji)
	case "d":
		if len(p.rows) > 0 {
			p.rows = append(p.rows[:p.cursor], p.rows[p.cursor+1:]...)
			if p.cursor >= len(p.rows) && p.cursor > 0 {
				p.cursor--
			}
		}
	case "enter":
		p.done = true
		return p, tea.Quit
	case "esc", "q", "ctrl+c":
		p.canceled = true
		return p, tea.Quit
	}
	return p, nil
}

// beginEdit focuses the text input for one field. The field starts empty with
// the current value shown as placeholder, so pressing Enter on an empty input
// keeps the old value and a typed value replaces it.
func (p picker) beginEdit(f editField) picker {
	p.editing = f
	ti := textinput.New()
	ti.Focus()
	switch f {
	case fieldEmoji:
		ti.Prompt = "emoji: "
		ti.Placeholder = p.rows[p.cursor].Emoji
	case fieldName:
		ti.Prompt = "name: "
		ti.Placeholder = p.rows[p.cursor].Name
	case fieldHint:
		ti.Prompt = "hint: "
		ti.Placeholder = p.rows[p.cursor].Hint
	}
	p.input = ti
	return p
}

func (p picker) updateEditing(km tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch km.Type {
	case tea.KeyEnter:
		val := strings.TrimSpace(p.input.Value())
		switch p.editing {
		case fieldEmoji:
			if val != "" {
				p.rows[p.cursor].Emoji = val
			}
			return p.beginEdit(fieldName), nil
		case fieldName:
			if val != "" {
				p.rows[p.cursor].Name = val
			}
			return p.beginEdit(fieldHint), nil
		case fieldHint:
			if val != "" {
				p.rows[p.cursor].Hint = val
			}
			return p.endEdit(), nil
		}
	case tea.KeyEsc:
		return p.endEdit(), nil
	}
	var cmd tea.Cmd
	p.input, cmd = p.input.Update(km)
	return p, cmd
}

// endEdit leaves edit mode and drops the current row if it ended up with no
// name (covers add-then-abandon).
func (p picker) endEdit() picker {
	p.editing = fieldNone
	p.input.Blur()
	if p.cursor < len(p.rows) && strings.TrimSpace(p.rows[p.cursor].Name) == "" {
		p.rows = append(p.rows[:p.cursor], p.rows[p.cursor+1:]...)
		if p.cursor >= len(p.rows) && p.cursor > 0 {
			p.cursor--
		}
	}
	return p
}

// result is the chosen sections: included rows with a non-empty name, in
// display order. Returns nil when the user canceled.
func (p picker) result() []Section {
	if p.canceled {
		return nil
	}
	var out []Section
	for _, r := range p.rows {
		if r.included && strings.TrimSpace(r.Name) != "" {
			out = append(out, r.Section)
		}
	}
	return out
}

var (
	pickerCursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(colorHeading)).Bold(true)
	pickerHintStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(colorStatusFg))
	pickerHelpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(colorStatusFg))
)

func (p picker) View() string {
	var b strings.Builder
	b.WriteString("  Customize your sidecar sections\n")
	b.WriteString("  " + pickerHelpStyle.Render("jk move · space toggle · J/K reorder · e edit · a add · d delete · ⏎ done") + "\n\n")
	for i, r := range p.rows {
		cursor := "   "
		if i == p.cursor {
			cursor = pickerCursorStyle.Render(" ▸ ")
		}
		box := "[ ]"
		if r.included {
			box = "[x]"
		}
		line := cursor + box + " " + r.Section.label()
		if r.Hint != "" {
			line += "  " + pickerHintStyle.Render("— "+r.Hint)
		}
		b.WriteString(line + "\n")
	}
	if p.editing != fieldNone {
		b.WriteString("\n  " + p.input.View() + "\n")
	}
	return b.String()
}

// pickSections runs the interactive picker. On any error, cancel, or an empty
// result it returns the input sections unchanged.
func pickSections(sections []Section) []Section {
	m, err := tea.NewProgram(newPicker(sections)).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "sidecar: section picker:", err)
		return sections
	}
	res := m.(picker).result()
	if len(res) == 0 {
		return sections
	}
	return res
}
