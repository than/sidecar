package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// waitForEvent runs the watcher on path, mutates via write, and reports
// whether an event arrived before the deadline.
func waitForEvent(t *testing.T, path string, write func()) bool {
	t.Helper()
	events := make(chan struct{}, 8)
	go watchFile(path, func(tea.Msg) {
		select {
		case events <- struct{}{}:
		default:
		}
	})
	// Give the watcher time to register before touching the file.
	time.Sleep(300 * time.Millisecond)
	write()
	select {
	case <-events:
		return true
	case <-time.After(3 * time.Second):
		return false
	}
}

func TestWatchFileDirectFile(t *testing.T) {
	dir := t.TempDir()
	board := filepath.Join(dir, "sidecar.md")
	os.WriteFile(board, []byte("# a\n"), 0o644)

	if !waitForEvent(t, board, func() {
		os.WriteFile(board, []byte("# b\n"), 0o644)
	}) {
		t.Fatal("no event for a plain file")
	}
}

func TestWatchFileSymlinkedFile(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	link := filepath.Join(root, "work", ".sidecar")
	os.MkdirAll(real, 0o755)
	os.MkdirAll(link, 0o755)

	target := filepath.Join(real, "sidecar.md")
	os.WriteFile(target, []byte("# a\n"), 0o644)

	board := filepath.Join(link, "sidecar.md")
	if err := os.Symlink(target, board); err != nil {
		t.Fatal(err)
	}

	if !waitForEvent(t, board, func() {
		os.WriteFile(target, []byte("# b\n"), 0o644)
	}) {
		t.Fatal("no event for a symlinked board file")
	}
}

func TestWatchFileSymlinkedDir(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	work := filepath.Join(root, "work")
	os.MkdirAll(real, 0o755)
	os.MkdirAll(work, 0o755)

	target := filepath.Join(real, "sidecar.md")
	os.WriteFile(target, []byte("# a\n"), 0o644)

	linkDir := filepath.Join(work, ".sidecar")
	if err := os.Symlink(real, linkDir); err != nil {
		t.Fatal(err)
	}
	board := filepath.Join(linkDir, "sidecar.md")

	if !waitForEvent(t, board, func() {
		os.WriteFile(target, []byte("# b\n"), 0o644)
	}) {
		t.Fatal("no event for a board under a symlinked directory")
	}
}

// A symlink whose name differs from its target's name: events in the target
// directory carry the target's base name.
func TestWatchFileSymlinkRenamedTarget(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	work := filepath.Join(root, "work")
	os.MkdirAll(real, 0o755)
	os.MkdirAll(work, 0o755)

	target := filepath.Join(real, "board.md")
	os.WriteFile(target, []byte("# a\n"), 0o644)

	board := filepath.Join(work, "sidecar.md")
	if err := os.Symlink(target, board); err != nil {
		t.Fatal(err)
	}

	if !waitForEvent(t, board, func() {
		os.WriteFile(target, []byte("# b\n"), 0o644)
	}) {
		t.Fatal("no event when the symlink name differs from the target name")
	}
}

// Editors and agent write tools replace the file by rename. Through a
// symlink the inode changes underneath, so the watcher must still fire.
func TestWatchFileSymlinkedFileAtomicRename(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	work := filepath.Join(root, "work")
	os.MkdirAll(real, 0o755)
	os.MkdirAll(work, 0o755)

	target := filepath.Join(real, "sidecar.md")
	os.WriteFile(target, []byte("# a\n"), 0o644)

	board := filepath.Join(work, "sidecar.md")
	if err := os.Symlink(target, board); err != nil {
		t.Fatal(err)
	}

	if !waitForEvent(t, board, func() {
		tmp := filepath.Join(real, ".sidecar.md.tmp")
		os.WriteFile(tmp, []byte("# b\n"), 0o644)
		os.Rename(tmp, target)
	}) {
		t.Fatal("no event for a rename-swap behind a symlink")
	}
}

// Two rename-swaps in a row: the second must still be reported. A watch
// bound to the symlink's original target inode goes stale after the first.
func TestWatchFileSymlinkedFileRepeatedRenames(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	work := filepath.Join(root, "work")
	os.MkdirAll(real, 0o755)
	os.MkdirAll(work, 0o755)

	target := filepath.Join(real, "sidecar.md")
	os.WriteFile(target, []byte("# a\n"), 0o644)

	board := filepath.Join(work, "sidecar.md")
	if err := os.Symlink(target, board); err != nil {
		t.Fatal(err)
	}

	swap := func(body string) {
		tmp := filepath.Join(real, ".sidecar.md.tmp")
		os.WriteFile(tmp, []byte(body), 0o644)
		os.Rename(tmp, target)
	}

	events := make(chan struct{}, 8)
	go watchFile(board, func(tea.Msg) {
		select {
		case events <- struct{}{}:
		default:
		}
	})
	time.Sleep(300 * time.Millisecond)

	for i, body := range []string{"# b\n", "# c\n"} {
		swap(body)
		select {
		case <-events:
		case <-time.After(3 * time.Second):
			t.Fatalf("no event for swap %d", i+1)
		}
	}
}
