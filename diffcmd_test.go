package main

import (
	"fmt"
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

// Q4: `sidecar diff -h`/`--help` must print usage and exit 0, not fall
// through to the unknown-flag rejection.
func TestRunDiffHelpFlag(t *testing.T) {
	for _, flag := range []string{"-h", "--help"} {
		var code int
		errOut := captureStderr(t, func() {
			out := captureStdout(t, func() {
				code = runDiff([]string{flag})
			})
			if !strings.Contains(out, "usage: sidecar diff") {
				t.Errorf("%s: out = %q, want usage line", flag, out)
			}
		})
		if code != 0 {
			t.Errorf("%s: exit = %d, want 0", flag, code)
		}
		if errOut != "" {
			t.Errorf("%s: stderr = %q, want empty", flag, errOut)
		}
	}
}

// S2: a large diff must be capped at 100 printed lines — the cap is on what
// enters the prompt, so it applies to both the semantic and fallback
// (unifiedU0) paths. Uses an unparseable 300-line change to force the
// fallback path, whose output (1 header + 300 dels + 300 adds) comfortably
// exceeds the cap.
func TestRunDiffCapsLargeOutput(t *testing.T) {
	dir := t.TempDir()
	board := filepath.Join(dir, sidecarDirName, "sidecar.md")
	os.MkdirAll(filepath.Join(dir, sidecarDirName), 0o755)

	const n = 300
	oldLines := make([]string, n)
	newLines := make([]string, n)
	for i := 0; i < n; i++ {
		oldLines[i] = fmt.Sprintf("line%d", i)
		newLines[i] = fmt.Sprintf("xline%d", i) // every line differs — no common prefix/suffix
	}
	oldRaw := strings.Join(oldLines, "\n") + "\n"
	newRaw := strings.Join(newLines, "\n") + "\n"

	os.WriteFile(board, []byte(oldRaw), 0o644)
	withWorkDir(t, dir, func() {
		captureStdout(t, func() { runDiff(nil) }) // seed snapshot
		os.WriteFile(board, []byte(newRaw), 0o644)
		out := captureStdout(t, func() { runDiff(nil) })
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

		full := diffLines(oldRaw, newRaw)
		if len(full) <= 100 {
			t.Fatalf("test setup didn't exceed the cap: %d raw diff lines", len(full))
		}

		// 100 capped lines + 1 tail line + 1 closing reminder line.
		wantTotal := 100 + 1 + 1
		if len(lines) != wantTotal {
			t.Fatalf("printed %d lines, want %d:\n%s", len(lines), wantTotal, out)
		}
		for i := 0; i < 100; i++ {
			if lines[i] != full[i] {
				t.Errorf("line %d = %q, want %q", i, lines[i], full[i])
			}
		}
		wantTail := fmt.Sprintf("… %d more lines — read the board", len(full)-100)
		if lines[100] != wantTail {
			t.Errorf("tail line = %q, want %q", lines[100], wantTail)
		}
		if !strings.Contains(lines[101], "the sidecar review queue") {
			t.Errorf("last line missing closing reminder: %q", lines[101])
		}
	})
}

// S3: a snapshot that exists but can't be read (e.g. EACCES) must not be
// silently treated as first-run and reseeded — that would quietly lose the
// baseline. It should report the error and leave the snapshot alone.
func TestRunDiffUnreadableSnapshotReportsErrorWithoutReseeding(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root — permission bits don't block reads")
	}
	dir := t.TempDir()
	board := filepath.Join(dir, sidecarDirName, "sidecar.md")
	os.MkdirAll(filepath.Join(dir, sidecarDirName), 0o755)
	os.WriteFile(board, []byte(diffBoardV1), 0o644)
	withWorkDir(t, dir, func() {
		captureStdout(t, func() { runDiff(nil) }) // seed snapshot normally
	})
	snap := filepath.Join(dir, sidecarDirName, "previous.md")
	before, err := os.ReadFile(snap)
	if err != nil {
		t.Fatalf("snapshot not seeded: %v", err)
	}
	// Write-only, no read: read fails (the case under test) while a write
	// would still succeed — so if runDiff wrongly falls through to
	// writeSnapshot on this error, that write succeeds and silently loses
	// the baseline instead of failing loudly, which chmod 000 would mask by
	// blocking the write too.
	if err := os.Chmod(snap, 0o200); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(snap, 0o644) // restore so t.TempDir() cleanup can remove it

	os.WriteFile(board, []byte(diffBoardV2), 0o644)
	withWorkDir(t, dir, func() {
		var code int
		var out string
		errOut := captureStderr(t, func() {
			out = captureStdout(t, func() { code = runDiff(nil) })
		})
		if code != 0 {
			t.Errorf("exit = %d, want 0", code)
		}
		if out != "" {
			t.Errorf("printed a diff despite an unreadable snapshot: %q", out)
		}
		if errOut == "" {
			t.Error("expected a stderr message for the unreadable snapshot")
		}
	})
	os.Chmod(snap, 0o644)
	after, err := os.ReadFile(snap)
	if err != nil {
		t.Fatalf("snapshot unreadable after restoring perms: %v", err)
	}
	if string(after) != string(before) {
		t.Error("snapshot was reseeded despite the read error — baseline lost")
	}
}

// S5: `sidecar diff a.md --help` silently used a.md and ignored the rest —
// trailing args must be rejected rather than dropped.
func TestRunDiffTooManyArgsRejected(t *testing.T) {
	dir := t.TempDir()
	withWorkDir(t, dir, func() {
		var code int
		errOut := captureStderr(t, func() {
			captureStdout(t, func() {
				code = runDiff([]string{"a.md", "--help"})
			})
		})
		if code != 2 {
			t.Errorf("exit = %d, want 2", code)
		}
		if !strings.Contains(errOut, "sidecar diff: too many arguments") {
			t.Errorf("stderr = %q, want it to mention too many arguments", errOut)
		}
	})
}

// T4: runDiff against a legacy root board (or any custom path not itself
// resident in .sidecar/) MkdirAlls a fresh .sidecar/ for its snapshot — the
// one place sidecar writes into a repo without arranging to be ignored.
// When that dir is newly created inside a git work tree, it must land in
// .git/info/exclude, same as init does for the default board.
func TestRunDiffExcludesFreshSnapshotDirForLegacyBoard(t *testing.T) {
	dir := t.TempDir()
	mustRun(t, dir, "git", "init", "-q")
	board := filepath.Join(dir, "SIDECAR.md")
	os.WriteFile(board, []byte(diffBoardV1), 0o644)
	withWorkDir(t, dir, func() {
		captureStdout(t, func() { runDiff([]string{"SIDECAR.md"}) })
	})
	data, err := os.ReadFile(filepath.Join(dir, ".git", "info", "exclude"))
	if err != nil || !strings.Contains(string(data), sidecarDirName+"/") {
		t.Fatalf("info/exclude = %q, err %v, want %s/", data, err, sidecarDirName)
	}
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

// P3: a board already living inside .sidecar/ but not named sidecar.md
// (e.g. .sidecar/notes.md) must not double up the directory into
// .sidecar/.sidecar/previous-<hash>.md — the snapshot belongs directly
// alongside it.
func TestSnapshotPathInsideSidecarDirNotNamedSidecarMd(t *testing.T) {
	boardAbs := filepath.Join("/repo", sidecarDirName, "notes.md")
	got := snapshotPath(boardAbs)
	if strings.Contains(got, filepath.Join(sidecarDirName, sidecarDirName)) {
		t.Errorf("snapshotPath doubled up the .sidecar dir: %q", got)
	}
	wantDir := filepath.Join("/repo", sidecarDirName)
	if filepath.Dir(got) != wantDir {
		t.Errorf("snapshot dir = %q, want %q", filepath.Dir(got), wantDir)
	}
	if !strings.HasPrefix(filepath.Base(got), "previous-") {
		t.Errorf("snapshot base = %q, want previous-<hash>.md", filepath.Base(got))
	}
}

// P3 end-to-end: running `sidecar diff .sidecar/notes.md` must not create a
// nested .sidecar/.sidecar/ directory.
func TestRunDiffCustomBoardInsideSidecarDirNoDoubleNesting(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, sidecarDirName), 0o755)
	board := filepath.Join(dir, sidecarDirName, "notes.md")
	os.WriteFile(board, []byte(diffBoardV1), 0o644)
	withWorkDir(t, dir, func() {
		captureStdout(t, func() { runDiff([]string{filepath.Join(sidecarDirName, "notes.md")}) })
	})
	if _, err := os.Stat(filepath.Join(dir, sidecarDirName, sidecarDirName)); err == nil {
		t.Error("nested .sidecar/.sidecar/ directory was created")
	}
	matches, _ := filepath.Glob(filepath.Join(dir, sidecarDirName, "previous-*.md"))
	if len(matches) != 1 {
		t.Fatalf("keyed snapshot files = %v, want exactly 1 directly in .sidecar/", matches)
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
