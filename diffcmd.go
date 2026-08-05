// diffcmd.go — the `sidecar diff` subcommand: print board changes since the
// last run, then advance the snapshot. Built for hook use — it never exits
// non-zero for an absent board, absent snapshot, or unchanged file.
package main

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
)

func runDiff(args []string) int {
	path := defaultBoardPath()
	if len(args) > 0 {
		path = args[0]
	}
	abs, err := filepath.Abs(expandTilde(path))
	if err != nil {
		fmt.Fprintln(os.Stderr, "sidecar diff:", err)
		return 1
	}

	raw, err := os.ReadFile(abs)
	if err != nil {
		return 0 // no board — silent, never an error in a hook
	}
	snap := snapshotPath(abs)
	prev, err := os.ReadFile(snap)
	if err != nil {
		writeSnapshot(snap, raw) // first run — seed silently
		return 0
	}
	if bytes.Equal(prev, raw) {
		return 0
	}

	for _, line := range diffLines(string(prev), string(raw)) {
		fmt.Println(line)
	}
	fmt.Println(closingReminder(path, string(raw)))
	writeSnapshot(snap, raw)
	return 0
}

// defaultBoardPath resolves the board for bare invocations: the .sidecar/
// home when present, the legacy root file when that's all there is, and the
// .sidecar/ home again as the target for fresh setups.
func defaultBoardPath() string {
	home := filepath.Join(sidecarDirName, "sidecar.md")
	if _, err := os.Stat(home); err == nil {
		return home
	}
	if _, err := os.Stat(defaultFile); err == nil {
		return defaultFile
	}
	return home
}

// snapshotPath is .sidecar/previous.md for the standard board, and a
// path-keyed previous-<hash>.md beside it for explicit board files.
func snapshotPath(boardAbs string) string {
	dir := filepath.Dir(boardAbs)
	if filepath.Base(dir) == sidecarDirName && filepath.Base(boardAbs) == "sidecar.md" {
		return filepath.Join(dir, "previous.md")
	}
	h := fnv.New32a()
	h.Write([]byte(boardAbs))
	return filepath.Join(dir, sidecarDirName, fmt.Sprintf("previous-%08x.md", h.Sum32()))
}

// writeSnapshot advances the snapshot, keeping exit 0 even when it fails (a
// hook must never fail the turn) but printing the error rather than
// swallowing it — otherwise a read-only checkout reprints the same diff
// forever with no explanation of why it never goes silent.
func writeSnapshot(path string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "sidecar diff:", err)
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "sidecar diff:", err)
	}
}

// closingReminder is the last line of a non-empty diff: the reconcile
// reminder, with section labels read from the board itself so custom
// sections stay accurate.
func closingReminder(rel, raw string) string {
	var labels []string
	if b, ok := parseBoard(raw); ok {
		for _, s := range b.Sections {
			labels = append(labels, s.Label)
		}
	}
	return reconcileMessageLabels(rel, labels)
}
