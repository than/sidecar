package main

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	tea "github.com/charmbracelet/bubbletea"
)

const debounce = 100 * time.Millisecond

// watchFile watches the file's parent directory (not the file itself) so
// atomic rename-swaps, deletes, and recreates all keep working: editors and
// Claude's Write tool replace the inode, so a file watch would go stale
// after the first save. Events for our filename are debounced and forwarded
// to the Bubble Tea program.
func watchFile(path string, send func(tea.Msg)) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	for {
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
