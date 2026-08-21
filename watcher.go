package main

import (
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"
)

const debounce = 100 * time.Millisecond

// watchFile watches the file's parent directory (not the file itself) so
// atomic rename-swaps, deletes, and recreates all keep working: editors and
// Claude's Write tool replace the inode, so a file watch would go stale
// after the first save. Events for our filename are debounced and forwarded
// to the Bubble Tea program.
//
// The path is resolved through symlinks on every retry, so a board that is
// a symlink — or lives under one, as when a git worktree points back at the
// main checkout's .sidecar — is watched where the writes actually land, and
// a retargeted symlink is picked up on the next restart.
func watchFile(path string, send func(tea.Msg)) {
	for {
		dir, base := resolve(path)

		w, err := fsnotify.NewWatcher()
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		// The directory itself may not exist yet — wait for it politely.
		for w.Add(dir) != nil {
			time.Sleep(time.Second)
		}

		var timer *time.Timer
	events:
		for {
			select {
			case ev, ok := <-w.Events:
				if !ok {
					break events
				}
				if strings.EqualFold(filepath.Base(ev.Name), base) {
					if timer != nil {
						timer.Stop()
					}
					timer = time.AfterFunc(debounce, func() {
						send(fileEventMsg{})
					})
				}
				// If the watched directory itself vanished, start over.
				if filepath.Clean(ev.Name) == filepath.Clean(dir) &&
					ev.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
					break events
				}
			case _, ok := <-w.Errors:
				if !ok {
					break events
				}
			}
		}
		w.Close()
	}
}

// resolve returns the directory to watch and the file name to match for
// path, following symlinks on both the file and its parent. Each step falls
// back to the unresolved value: the board, or even its directory, may not
// exist yet.
func resolve(path string) (dir, base string) {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Dir(real), filepath.Base(real)
	}
	dir, base = filepath.Dir(path), filepath.Base(path)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	return dir, base
}
