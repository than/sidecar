package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withWorkDir runs f inside dir so relative default-path logic is exercised.
func withWorkDir(t *testing.T, dir string, f func()) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	f()
}

// captureStdout runs f and returns everything it printed.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	f()
	w.Close()
	os.Stdout = old
	buf := make([]byte, 1<<16)
	n, _ := r.Read(buf)
	return string(buf[:n])
}

// captureStderr runs f and returns everything it wrote to stderr.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	f()
	w.Close()
	os.Stderr = old
	buf := make([]byte, 1<<16)
	n, _ := r.Read(buf)
	return string(buf[:n])
}

const diffBoardV1 = "## 🧠 Needs action\n\n- Review PR #7\n\n## ✅ Done\n\n- nothing yet\n"
const diffBoardV2 = "## 🧠 Needs action\n\n- nothing left\n\n## ✅ Done\n\n- Review PR #7\n"

func TestRunDiffFirstRunSilent(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, sidecarDirName), 0o755)
	os.WriteFile(filepath.Join(dir, sidecarDirName, "sidecar.md"), []byte(diffBoardV1), 0o644)
	withWorkDir(t, dir, func() {
		out := captureStdout(t, func() {
			if code := runDiff(nil); code != 0 {
				t.Errorf("exit = %d", code)
			}
		})
		if out != "" {
			t.Errorf("first run printed %q", out)
		}
	})
	if _, err := os.Stat(filepath.Join(dir, sidecarDirName, "previous.md")); err != nil {
		t.Error("snapshot not written on first run")
	}
}

func TestRunDiffReportsMoveThenGoesSilent(t *testing.T) {
	dir := t.TempDir()
	board := filepath.Join(dir, sidecarDirName, "sidecar.md")
	os.MkdirAll(filepath.Join(dir, sidecarDirName), 0o755)
	os.WriteFile(board, []byte(diffBoardV1), 0o644)
	withWorkDir(t, dir, func() {
		captureStdout(t, func() { runDiff(nil) }) // first run seeds snapshot
		os.WriteFile(board, []byte(diffBoardV2), 0o644)
		out := captureStdout(t, func() { runDiff(nil) })
		if !strings.Contains(out, `moved 🧠→✅: "Review PR #7"`) {
			t.Errorf("missing move line:\n%s", out)
		}
		if !strings.Contains(out, "the sidecar review queue") {
			t.Errorf("missing closing reminder:\n%s", out)
		}
		// Snapshot advanced: an immediate re-run prints nothing.
		if again := captureStdout(t, func() { runDiff(nil) }); again != "" {
			t.Errorf("second run printed %q", again)
		}
	})
}

// R4: an unparseable board (no ## headings) has no section labels, so the
// closing reminder's last line must drop the "Sections:" clause rather than
// render "Sections: ." — but must still carry the sentinel phrase.
func TestRunDiffUnparseableBoardOmitsSectionsClause(t *testing.T) {
	dir := t.TempDir()
	board := filepath.Join(dir, sidecarDirName, "sidecar.md")
	os.MkdirAll(filepath.Join(dir, sidecarDirName), 0o755)
	os.WriteFile(board, []byte("plain text, no headings\n"), 0o644)
	withWorkDir(t, dir, func() {
		captureStdout(t, func() { runDiff(nil) }) // first run seeds snapshot
		os.WriteFile(board, []byte("different plain text, still no headings\n"), 0o644)
		out := captureStdout(t, func() { runDiff(nil) })
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		last := lines[len(lines)-1]
		if !strings.Contains(last, "the sidecar review queue") {
			t.Errorf("last line missing sentinel: %q", last)
		}
		if strings.Contains(last, "Sections:") {
			t.Errorf("last line should omit Sections clause: %q", last)
		}
	})
}

// P1: only ENOENT is silent — any other ReadFile error (e.g. the board path
// pointing at a directory) must exit 0 (never fail a hook) but report the
// error on stderr rather than silently swallowing it.
func TestRunDiffBoardPathIsDirectoryReportsError(t *testing.T) {
	dir := t.TempDir()
	boardDir := filepath.Join(dir, sidecarDirName)
	os.MkdirAll(filepath.Join(boardDir, "sidecar.md"), 0o755) // a dir named sidecar.md
	withWorkDir(t, dir, func() {
		var code int
		errOut := captureStderr(t, func() {
			captureStdout(t, func() {
				code = runDiff(nil)
			})
		})
		if code != 0 {
			t.Errorf("exit = %d, want 0", code)
		}
		if errOut == "" {
			t.Error("expected a stderr message for a non-ENOENT ReadFile error, got none")
		}
	})
}

// P4: `sidecar diff --oops` must not be silently treated as a board path —
// reject any first arg starting with "-" as an unknown flag.
func TestRunDiffUnknownFlagRejected(t *testing.T) {
	dir := t.TempDir()
	withWorkDir(t, dir, func() {
		var code int
		errOut := captureStderr(t, func() {
			captureStdout(t, func() {
				code = runDiff([]string{"--oops"})
			})
		})
		if code != 2 {
			t.Errorf("exit = %d, want 2", code)
		}
		if !strings.Contains(errOut, `unknown flag "--oops"`) {
			t.Errorf("stderr = %q, want it to mention the unknown flag", errOut)
		}
	})
}

func TestRunDiffMissingBoardSilent(t *testing.T) {
	withWorkDir(t, t.TempDir(), func() {
		out := captureStdout(t, func() {
			if code := runDiff(nil); code != 0 {
				t.Errorf("exit = %d", code)
			}
		})
		if out != "" {
			t.Errorf("printed %q for a missing board", out)
		}
	})
}

func TestRunDiffExplicitPathKeyedSnapshot(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "NOTES.md")
	os.WriteFile(file, []byte(diffBoardV1), 0o644)
	withWorkDir(t, dir, func() {
		captureStdout(t, func() { runDiff([]string{"NOTES.md"}) })
	})
	matches, _ := filepath.Glob(filepath.Join(dir, sidecarDirName, "previous-*.md"))
	if len(matches) != 1 {
		t.Fatalf("keyed snapshot files = %v, want exactly 1", matches)
	}
}

func TestDefaultBoardPathPrefersSidecarDir(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, sidecarDirName), 0o755)
	os.WriteFile(filepath.Join(dir, sidecarDirName, "sidecar.md"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "SIDECAR.md"), []byte("y"), 0o644)
	withWorkDir(t, dir, func() {
		if got := defaultBoardPath(); got != filepath.Join(sidecarDirName, "sidecar.md") {
			t.Errorf("got %q", got)
		}
	})
}

func TestDefaultBoardPathLegacyFallback(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "SIDECAR.md"), []byte("y"), 0o644)
	withWorkDir(t, dir, func() {
		if got := defaultBoardPath(); got != "SIDECAR.md" {
			t.Errorf("got %q", got)
		}
	})
}

func TestDefaultBoardPathFreshDefault(t *testing.T) {
	withWorkDir(t, t.TempDir(), func() {
		if got := defaultBoardPath(); got != filepath.Join(sidecarDirName, "sidecar.md") {
			t.Errorf("got %q", got)
		}
	})
}
