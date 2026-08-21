package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRepo builds a main checkout plus named linked worktrees, using the
// same on-disk layout git writes: a .git directory in the main checkout, a
// .git file in each worktree, and a gitdir pointer back from each entry in
// .git/worktrees.
func fakeRepo(t *testing.T, names ...string) (main string, worktrees map[string]string) {
	t.Helper()
	root := t.TempDir()
	main = filepath.Join(root, "main")
	if err := os.MkdirAll(filepath.Join(main, ".git", "worktrees"), 0o755); err != nil {
		t.Fatal(err)
	}
	worktrees = map[string]string{}
	for _, n := range names {
		wt := filepath.Join(root, n)
		if err := os.MkdirAll(wt, 0o755); err != nil {
			t.Fatal(err)
		}
		entry := filepath.Join(main, ".git", "worktrees", n)
		if err := os.MkdirAll(entry, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(wt, ".git"), "gitdir: "+entry+"\n")
		writeFile(t, filepath.Join(entry, "gitdir"), filepath.Join(wt, ".git")+"\n")
		worktrees[n] = wt
	}
	return main, worktrees
}

func writeBoard(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, sidecarDirName, "sidecar.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, p, body)
	return p
}

func TestMainCheckout(t *testing.T) {
	main, wts := fakeRepo(t, "app")

	if got := mainCheckout(wts["app"]); got != main {
		t.Errorf("mainCheckout(worktree) = %q, want %q", got, main)
	}
	if got := mainCheckout(main); got != "" {
		t.Errorf("mainCheckout(main checkout) = %q, want \"\"", got)
	}
	if got := mainCheckout(t.TempDir()); got != "" {
		t.Errorf("mainCheckout(no repo) = %q, want \"\"", got)
	}
}

// A .git file that isn't a worktree pointer (a submodule names a gitdir with
// no /worktrees/ segment) must not be mistaken for one.
func TestMainCheckoutIgnoresNonWorktreeGitFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".git"), "gitdir: "+filepath.Join(dir, "..", ".git", "modules", "sub")+"\n")
	if got := mainCheckout(dir); got != "" {
		t.Errorf("mainCheckout(submodule) = %q, want \"\"", got)
	}
}

func TestWorktreeRoots(t *testing.T) {
	main, wts := fakeRepo(t, "app", "billing")
	got := worktreeRoots(main)
	if len(got) != 2 {
		t.Fatalf("worktreeRoots = %v, want 2 entries", got)
	}
	for _, want := range []string{wts["app"], wts["billing"]} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("worktreeRoots missing %q (got %v)", want, got)
		}
	}
}

// The whole point: a worktree with no board of its own inherits the main
// checkout's rather than starting a second one nobody is watching.
func TestDefaultBoardPathInheritsMainCheckoutBoard(t *testing.T) {
	main, wts := fakeRepo(t, "app")
	want := writeBoard(t, main, "# board\n")

	withWorkDir(t, wts["app"], func() {
		if got := defaultBoardPath(); got != want {
			t.Errorf("defaultBoardPath() = %q, want the main checkout's board %q", got, want)
		}
	})
}

// A worktree that already has its own board keeps it — inheriting would
// orphan whatever is already written there.
func TestDefaultBoardPathKeepsLocalBoard(t *testing.T) {
	main, wts := fakeRepo(t, "app")
	writeBoard(t, main, "# main\n")
	writeBoard(t, wts["app"], "# local\n")

	withWorkDir(t, wts["app"], func() {
		want := filepath.Join(sidecarDirName, "sidecar.md")
		if got := defaultBoardPath(); got != want {
			t.Errorf("defaultBoardPath() = %q, want the local board %q", got, want)
		}
	})
}

// No board anywhere: the worktree is still where a fresh one gets created.
func TestDefaultBoardPathNoBoardAnywhere(t *testing.T) {
	_, wts := fakeRepo(t, "app")
	withWorkDir(t, wts["app"], func() {
		want := filepath.Join(sidecarDirName, "sidecar.md")
		if got := defaultBoardPath(); got != want {
			t.Errorf("defaultBoardPath() = %q, want %q", got, want)
		}
	})
}

func TestSplitBoards(t *testing.T) {
	main, wts := fakeRepo(t, "app", "billing")
	mainBoard := writeBoard(t, main, "# main\n")
	appBoard := writeBoard(t, wts["app"], "# app\n")

	got := splitBoards(appBoard)
	if len(got) != 1 || got[0] != mainBoard {
		t.Errorf("splitBoards(app board) = %v, want [%s]", got, mainBoard)
	}
	if got := splitBoards(mainBoard); len(got) != 1 || got[0] != appBoard {
		t.Errorf("splitBoards(main board) = %v, want [%s]", got, appBoard)
	}
	// billing has no board, so it contributes nothing.
	if got := splitBoards(writeBoard(t, wts["billing"], "# billing\n")); len(got) != 2 {
		t.Errorf("splitBoards(billing board) = %v, want both siblings", got)
	}
}

// A worktree board symlinked at the main checkout's board is one board, not
// two — that's the fix, not the problem.
func TestSplitBoardsIgnoresSymlinkedSameFile(t *testing.T) {
	main, wts := fakeRepo(t, "app")
	mainBoard := writeBoard(t, main, "# main\n")

	link := filepath.Join(wts["app"], sidecarDirName, "sidecar.md")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(mainBoard, link); err != nil {
		t.Fatal(err)
	}

	if got := splitBoards(link); len(got) != 0 {
		t.Errorf("splitBoards(symlinked board) = %v, want none", got)
	}
}

// The agent-facing channel: a diff run from a worktree names its own board
// and says another one is live.
func TestRunDiffWarnsAboutSplitBoards(t *testing.T) {
	main, wts := fakeRepo(t, "app")
	writeBoard(t, main, "# main\n")
	appBoard := writeBoard(t, wts["app"], "# app\n\n## 🧠 Needs you\n\n- alpha\n")

	withWorkDir(t, wts["app"], func() {
		captureStdout(t, func() { runDiff(nil) }) // seed
		writeFile(t, appBoard, "# app\n\n## 🧠 Needs you\n\n- beta\n")
		out := captureStdout(t, func() { runDiff(nil) })

		if !strings.Contains(out, "board: ") {
			t.Errorf("diff didn't name the board it read:\n%s", out)
		}
		if !strings.Contains(out, "a second board exists at") {
			t.Errorf("diff didn't warn about the main checkout's board:\n%s", out)
		}
	})
}

func TestBoardLabel(t *testing.T) {
	cases := map[string]string{
		filepath.Join("/Users/t/munks", sidecarDirName, "sidecar.md"):  "munks/sidecar.md",
		filepath.Join("/repos/adjustmunk", sidecarDirName, "board.md"): "adjustmunk/board.md",
		filepath.Join("/Users/t/munks", "SIDECAR.md"):                  "munks/SIDECAR.md",
		"/sidecar.md": "sidecar.md",
	}
	for in, want := range cases {
		if got := boardLabel(in); got != want {
			t.Errorf("boardLabel(%q) = %q, want %q", in, got, want)
		}
	}
}
