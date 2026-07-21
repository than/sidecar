package main

import (
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
	for _, marker := range []string{"🔴", "🟡", "🟢"} {
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
