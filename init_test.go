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

// A fresh project (no settings.json) gets a UserPromptSubmit hook written.
func TestReconcileHookFreshFile(t *testing.T) {
	root := t.TempDir()
	writeReconcileHook(root, "SIDECAR.md")

	data, err := os.ReadFile(filepath.Join(root, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s map[string]any
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("settings.json not valid JSON: %v\n%s", err, data)
	}
	hooks := s["hooks"].(map[string]any)
	if _, ok := hooks["UserPromptSubmit"]; !ok {
		t.Errorf("no UserPromptSubmit hook:\n%s", data)
	}
	if _, ok := hooks["SessionStart"]; ok {
		t.Errorf("unexpected SessionStart hook:\n%s", data)
	}
	if !strings.Contains(string(data), "sidecar SIDECAR.md") {
		t.Errorf("hook missing sidecar reference:\n%s", data)
	}
}

// Merging preserves a user's own hooks, upgrades an old sidecar SessionStart
// hook to UserPromptSubmit, and is idempotent across re-runs.
func TestReconcileHookMergesAndUpgrades(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.json")

	// Pre-existing settings: a user hook plus an old sidecar SessionStart hook.
	existing := map[string]any{
		"model": "opus",
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "echo keep-me"}}},
				map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "echo Maintain SIDECAR.md as " + hookSentinel}}},
			},
		},
	}
	b, _ := json.MarshalIndent(existing, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}

	writeReconcileHook(root, "SIDECAR.md")
	writeReconcileHook(root, "SIDECAR.md") // second run must not duplicate

	data, _ := os.ReadFile(path)
	var s map[string]any
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("settings.json not valid JSON: %v\n%s", err, data)
	}
	if s["model"] != "opus" {
		t.Errorf("unrelated key 'model' lost:\n%s", data)
	}
	if !strings.Contains(string(data), "keep-me") {
		t.Errorf("user's own hook was dropped:\n%s", data)
	}
	hooks := s["hooks"].(map[string]any)
	if _, ok := hooks["SessionStart"]; !ok {
		t.Errorf("user's SessionStart hook removed entirely:\n%s", data)
	}
	ups, ok := hooks["UserPromptSubmit"].([]any)
	if !ok || len(ups) != 1 {
		t.Fatalf("want exactly 1 UserPromptSubmit entry, got:\n%s", data)
	}
	// The old sidecar SessionStart hook should be gone (only keep-me remains).
	if n := strings.Count(string(data), hookSentinel); n != 1 {
		t.Errorf("sentinel appears %d times, want 1 (old hook not replaced):\n%s", n, data)
	}
}

// A settings.json that isn't valid JSON is left untouched.
func TestReconcileHookLeavesInvalidJSON(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeReconcileHook(root, "SIDECAR.md")

	data, _ := os.ReadFile(path)
	if string(data) != "{not json" {
		t.Errorf("invalid settings.json was modified: %q", data)
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
