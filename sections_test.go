// sections_test.go
package main

import (
	"strings"
	"testing"
)

func TestSectionHeader(t *testing.T) {
	cases := []struct {
		in   Section
		want string
	}{
		{Section{"🧠", "Needs action", "x"}, "## 🧠 Needs action"},
		{Section{"", "Todo", ""}, "## Todo"},
	}
	for _, c := range cases {
		if got := c.in.Header(); got != c.want {
			t.Errorf("Header() = %q, want %q", got, c.want)
		}
	}
}

func TestSectionLabel(t *testing.T) {
	if got := (Section{"✅", "Done", ""}).label(); got != "✅ Done" {
		t.Errorf("label() = %q, want %q", got, "✅ Done")
	}
}

func TestDefaultSections(t *testing.T) {
	got := defaultSections()
	if len(got) != 5 {
		t.Fatalf("defaultSections len = %d, want 5", len(got))
	}
	wantEmoji := []string{"🧠", "🚧", "🚘", "✅", "📦"}
	for i, e := range wantEmoji {
		if got[i].Emoji != e {
			t.Errorf("section %d emoji = %q, want %q", i, got[i].Emoji, e)
		}
		if got[i].Name == "" || got[i].Hint == "" {
			t.Errorf("section %d missing name/hint: %+v", i, got[i])
		}
	}
}

func TestRenderTemplateDefault(t *testing.T) {
	out := renderTemplate(defaultSections())
	if !strings.HasPrefix(out, "# Sidecar\n") {
		t.Errorf("template missing title:\n%s", out)
	}
	for _, s := range defaultSections() {
		if !strings.Contains(out, s.Header()+"\n") {
			t.Errorf("template missing header %q:\n%s", s.Header(), out)
		}
		if !strings.Contains(out, "· "+s.label()+" = "+s.Hint) {
			t.Errorf("template comment missing hint for %q:\n%s", s.Name, out)
		}
	}
	if strings.Count(out, "- nothing yet") != 5 {
		t.Errorf("want 5 placeholder bullets, got %d", strings.Count(out, "- nothing yet"))
	}
	if !strings.HasSuffix(out, "\n") || strings.HasSuffix(out, "\n\n") {
		t.Errorf("template must end in exactly one newline:\n%q", out[len(out)-3:])
	}
}

func TestRenderTemplateCustomNoHint(t *testing.T) {
	out := renderTemplate([]Section{{"", "Todo", ""}})
	if !strings.Contains(out, "## Todo\n") {
		t.Errorf("missing text-only header:\n%s", out)
	}
	if strings.Contains(out, "Todo = ") {
		t.Errorf("hintless section should have no '= meaning' line:\n%s", out)
	}
}
