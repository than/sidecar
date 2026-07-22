package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runInit scaffolds the file with the starter template when it's absent, and
// (outside a git work tree) doesn't touch stdin.
func TestInitScaffolds(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "SIDECAR.md")

	if code := runInit([]string{target}); code != 0 {
		t.Fatalf("runInit exit code = %d, want 0", code)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "# Sidecar") {
		t.Errorf("scaffolded file missing header:\n%s", data)
	}
	for _, marker := range []string{"🧠", "🚧", "✅", "📦"} {
		if !strings.Contains(string(data), marker) {
			t.Errorf("template missing section marker %q", marker)
		}
	}
}

// An existing file is left untouched.
func TestInitLeavesExistingFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "SIDECAR.md")
	if err := os.WriteFile(target, []byte("# Mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := runInit([]string{target}); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "# Mine\n" {
		t.Errorf("existing file overwritten: %q", data)
	}
}

func TestWriteClaudeNoteAppendsAndDedupes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("# Existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeClaudeNote(root, "SIDECAR.md")
	data, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	got := string(data)
	for _, want := range []string{"# Existing", claudeNoteMarker, "sidecar SIDECAR.md", "go install github.com/than/sidecar@latest", "🧠"} {
		if !strings.Contains(got, want) {
			t.Errorf("CLAUDE.md missing %q:\n%s", want, got)
		}
	}

	// Second call must not duplicate the note.
	writeClaudeNote(root, "SIDECAR.md")
	data, _ = os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if n := strings.Count(string(data), "<!-- "+claudeNoteMarker+" -->"); n != 1 {
		t.Errorf("note written %d times, want 1", n)
	}
}

func TestSessionHookJSONValid(t *testing.T) {
	out := sessionHookJSON("SIDECAR.md")
	var parsed map[string]any
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("sessionHookJSON is not valid JSON: %v\n%s", err, out)
	}
	if !strings.Contains(out, "SessionStart") || !strings.Contains(out, "sidecar SIDECAR.md") {
		t.Errorf("hook JSON missing expected fields:\n%s", out)
	}
}

func TestAppendLineDedupAndNewline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exclude")

	// Missing trailing newline should be repaired before appending.
	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := appendLine(path, "SIDECAR.md"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "existing\nSIDECAR.md\n" {
		t.Errorf("got %q, want %q", got, "existing\nSIDECAR.md\n")
	}

	// Second identical append is a no-op.
	if err := appendLine(path, "SIDECAR.md"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); strings.Count(string(got), "SIDECAR.md") != 1 {
		t.Errorf("duplicate line written: %q", got)
	}
}
