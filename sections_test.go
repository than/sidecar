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
	if !strings.Contains(out, "Prune early sections") {
		t.Errorf("template missing prune instruction:\n%s", out)
	}
	if !strings.Contains(out, "Never hard-wrap entry text") {
		t.Errorf("template missing no-hard-wrap instruction:\n%s", out)
	}
	if !strings.HasSuffix(out, "\n") || strings.HasSuffix(out, "\n\n") {
		t.Errorf("template must end in exactly one newline:\n%q", out[len(out)-3:])
	}
}

// U3: leadingEmoji's old `r > 0x2600` catch-all matched CJK/kana/Hangul —
// "完了 tasks" isn't emoji-led, and must be treated as a text-only label.
func TestLeadingEmojiRejectsCJK(t *testing.T) {
	if emoji, ok := leadingEmoji("完了 tasks"); ok {
		t.Errorf("leadingEmoji(%q) = %q, true — want no emoji detected for CJK text", "完了 tasks", emoji)
	}
	if got := sectionTag("完了 tasks"); got != "完了 tasks" {
		t.Errorf("sectionTag(CJK) = %q, want the full label", got)
	}
	if got := sectionFromLabel("完了 tasks").Emoji; got != "" {
		t.Errorf("sectionFromLabel(CJK).Emoji = %q, want empty", got)
	}
}

// U3 regression guard: real emoji sections must still be detected.
func TestLeadingEmojiStillAcceptsRealEmoji(t *testing.T) {
	if emoji, ok := leadingEmoji("🧠 Needs action"); !ok || emoji != "🧠" {
		t.Errorf("leadingEmoji(🧠 Needs action) = %q, %v, want 🧠, true", emoji, ok)
	}
	if got := sectionTag("🧠 Needs action"); got != "🧠" {
		t.Errorf("sectionTag(🧠 Needs action) = %q, want 🧠", got)
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
