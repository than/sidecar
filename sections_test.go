// sections_test.go
package main

import "testing"

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
