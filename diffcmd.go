// diffcmd.go — the `sidecar diff` subcommand: print board changes since the
// last run, then advance the snapshot. Built for hook use — it never exits
// non-zero for an absent board, absent snapshot, or unchanged file.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
)

func runDiff(args []string) int {
	operands := 0
	for _, a := range args {
		if a != "--mid-turn" {
			operands++
		}
	}
	if operands > 1 {
		fmt.Fprintln(os.Stderr, "sidecar diff: too many arguments")
		return 2
	}
	path := defaultBoardPath()
	midTurn, havePath := false, false
	for _, a := range args {
		switch {
		case a == "-h" || a == "--help":
			fmt.Println("usage: sidecar diff [--mid-turn] [file.md]")
			fmt.Println("Prints board changes since the last run.")
			fmt.Println("--mid-turn prints only the human's replies and ticks, as PostToolUse hook JSON, and stays silent otherwise.")
			return 0
		case a == "--mid-turn":
			midTurn = true
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "sidecar diff: unknown flag %q\n", a)
			return 2
		case havePath:
			fmt.Fprintln(os.Stderr, "sidecar diff: too many arguments")
			return 2
		default:
			path, havePath = a, true
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
	if midTurn {
		return midTurnReport(path, snap, string(prev), string(raw))
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
	if err := writeFileAtomic(path, data); err != nil {
		fmt.Fprintln(os.Stderr, "sidecar diff:", err)
		return
	}

	// A legacy or custom board not itself resident in .sidecar/ gets a fresh
	// .sidecar/ MkdirAll'd here for its snapshot — the one place sidecar
	// writes into a repo without arranging to be ignored. Exclude it now,
	// the same as init does for the default board — but this runs on every
	// plain `sidecar diff` hook invocation, so it must stay silent on
	// stdout like the rest of this path (verbose=false); errors still
	// surface via writeIgnore's own stderr print.
	if freshDir && filepath.Base(dir) == sidecarDirName {
		excludeSidecarDir(repoRoot(filepath.Dir(dir)), false)
	}
}

// writeFileAtomic writes data to path via a temp file in the same directory
// followed by a rename, instead of os.WriteFile's truncate-in-place. A hook
// killed mid-write (the process gets no graceful shutdown) would otherwise
// leave a half-written snapshot, and the next diff would compare against
// that garbage and dump bogus changes. The rename is same-directory, so it's
// atomic on any filesystem this runs on. The temp name's different basename
// also means the viewer's watcher (which only reacts to its exact watched
// filename, see watcher.go) stays quiet even if a crash leaves one behind.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
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

// midTurnReport is the PostToolUse half of the hook cycle: it surfaces only
// what the human did in the viewer — a reply, a tick — as hook JSON that adds
// context to the running turn. Anything else stays silent and leaves the
// snapshot alone, so the per-prompt diff still reports it. The snapshot moves
// only when something was reported, so the same reply is never delivered twice.
func midTurnReport(path, snap, prev, raw string) int {
	ob, ok1 := parseBoard(prev)
	nb, ok2 := parseBoard(raw)
	if !ok1 || !ok2 {
		return 0
	}
	var lines []string
	for _, l := range semanticDiff(ob, nb) {
		if strings.Contains(l, " — replied ") || strings.Contains(l, " — ticked") || strings.Contains(l, " — unticked") {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return 0
	}
	msg := "The human answered on the sidecar board while you worked:\n" + strings.Join(cappedDiffLines(lines), "\n") +
		"\nRead " + path + ", act on the reply, then clear the item's Ask: and Answer: lines or move it."
	out, _ := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":     "PostToolUse",
		"additionalContext": msg,
	}})
	fmt.Println(string(out))
	writeSnapshot(snap, []byte(raw))
	return 0
}
