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
	"strings"
)

func runDiff(args []string) int {
	if len(args) > 1 {
		fmt.Fprintln(os.Stderr, "sidecar diff: too many arguments")
		return 2
	}
	path := defaultBoardPath()
	if len(args) > 0 {
		switch args[0] {
		case "-h", "--help":
			fmt.Println("usage: sidecar diff [file.md]")
			fmt.Println("Prints board changes since the last run.")
			return 0
		default:
			if strings.HasPrefix(args[0], "-") {
				fmt.Fprintf(os.Stderr, "sidecar diff: unknown flag %q\n", args[0])
				return 2
			}
			path = args[0]
		}
	}
	abs, err := filepath.Abs(expandTilde(path))
	if err != nil {
		fmt.Fprintln(os.Stderr, "sidecar diff:", err)
		return 1
	}

	raw, err := os.ReadFile(abs)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintln(os.Stderr, "sidecar diff:", err)
		}
		return 0
	}
	snap := snapshotPath(abs)
	prev, err := os.ReadFile(snap)
	if err != nil {
		if !os.IsNotExist(err) {
			// Some other read failure (e.g. EACCES) — writing would likely
			// fail too, and reseeding here would lose the baseline forever.
			// Report it and leave the snapshot untouched; the next run tries
			// again rather than silently treating this as a fresh start.
			fmt.Fprintln(os.Stderr, "sidecar diff:", err)
			return 0
		}
		writeSnapshot(snap, raw) // first run — seed silently
		return 0
	}
	if bytes.Equal(prev, raw) {
		return 0
	}

	for _, line := range cappedDiffLines(diffLines(string(prev), string(raw))) {
		fmt.Println(line)
	}
	fmt.Println(closingReminder(path, string(raw)))
	writeSnapshot(snap, raw)
	return 0
}

// maxDiffOutputLines caps what runDiff prints — the diff feeds straight into
// the model's prompt on every turn, so an enormous change (a rewrite, a
// paste) must not flood it; the board itself is always the source of truth.
const maxDiffOutputLines = 100

// cappedDiffLines truncates lines to maxDiffOutputLines, appending a tail
// line noting how many were omitted. Applies uniformly to both diffLines
// paths (semantic and unifiedU0 fallback) since the cap is on what enters
// the prompt, not on how the diff was computed.
func cappedDiffLines(lines []string) []string {
	if len(lines) <= maxDiffOutputLines {
		return lines
	}
	out := append([]string{}, lines[:maxDiffOutputLines]...)
	return append(out, fmt.Sprintf("… %d more lines — read the board", len(lines)-maxDiffOutputLines))
}

// defaultBoardPath resolves the board for bare invocations: the .sidecar/
// home when present, the legacy root file when that's all there is, and the
// .sidecar/ home again as the target for fresh setups.
func defaultBoardPath() string {
	home := filepath.Join(sidecarDirName, "sidecar.md")
	if _, err := os.Stat(home); err == nil {
		return home
	}
	if _, err := os.Stat(legacyFile); err == nil {
		return legacyFile
	}
	return home
}

// snapshotPath is .sidecar/previous.md for the standard board, and a
// path-keyed previous-<hash>.md beside it for explicit board files. When the
// board's own parent dir is already named .sidecar (e.g. .sidecar/notes.md),
// the keyed snapshot goes directly in that dir rather than doubling it up
// into .sidecar/.sidecar/.
func snapshotPath(boardAbs string) string {
	dir := filepath.Dir(boardAbs)
	if filepath.Base(dir) == sidecarDirName && filepath.Base(boardAbs) == "sidecar.md" {
		return filepath.Join(dir, "previous.md")
	}
	h := fnv.New32a()
	h.Write([]byte(boardAbs))
	name := fmt.Sprintf("previous-%08x.md", h.Sum32())
	if filepath.Base(dir) == sidecarDirName {
		return filepath.Join(dir, name)
	}
	return filepath.Join(dir, sidecarDirName, name)
}

// writeSnapshot advances the snapshot, keeping exit 0 even when it fails (a
// hook must never fail the turn) but printing the error rather than
// swallowing it — otherwise a read-only checkout reprints the same diff
// forever with no explanation of why it never goes silent.
func writeSnapshot(path string, data []byte) {
	dir := filepath.Dir(path)
	_, statErr := os.Stat(dir)
	freshDir := os.IsNotExist(statErr)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "sidecar diff:", err)
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "sidecar diff:", err)
		return
	}

	// A legacy or custom board not itself resident in .sidecar/ gets a fresh
	// .sidecar/ MkdirAll'd here for its snapshot — the one place sidecar
	// writes into a repo without arranging to be ignored. Exclude it now,
	// the same as init does for the default board; silent on failure (not a
	// git work tree, git missing) like the rest of this path, except the
	// confirmation excludeSidecarDir already prints via writeIgnore.
	if freshDir && filepath.Base(dir) == sidecarDirName {
		excludeSidecarDir(repoRoot(filepath.Dir(dir)))
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
