package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

// runInit scaffolds the target file and wires it into Claude Code. Every
// recommended default applies without asking: a legacy root SIDECAR.md is
// migrated, the board's home is git-excluded, and a CLAUDE.md note plus
// reconcile hook are written. --no-claude and --keep-board opt out of the
// Claude Code wiring and the migration, respectively. --yes/-y are accepted
// as no-op aliases for scripts and muscle memory written before this
// behavior became the default. Returns a process exit code.
func runInit(args []string) int {
	noClaude := false
	keepBoard := false
	var rest []string
	for _, a := range args {
		switch a {
		case "--yes", "-y":
			// No-op: bare init already installs every default.
		case "--no-claude":
			noClaude = true
		case "--keep-board":
			keepBoard = true
		case "-h", "--help":
			fmt.Println("usage: sidecar init [file.md]")
			fmt.Println("Creates the board and wires it into Claude Code — no prompts, every")
			fmt.Println("recommended default applies (migrate a legacy board, git-exclude,")
			fmt.Println("CLAUDE.md note + reconcile hook).")
			fmt.Println()
			fmt.Println("  --no-claude    skip the CLAUDE.md note and reconcile hook")
			fmt.Println("  --keep-board   skip migrating a legacy root SIDECAR.md")
			fmt.Println("  --yes, -y      accepted as a no-op — this is already the default")
			return 0
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(os.Stderr, "sidecar init: unknown flag %q\n", a)
				return 2
			}
			rest = append(rest, a)
		}
	}
	args = rest

	isDefaultTarget := len(args) == 0
	target := filepath.Join(sidecarDirName, "sidecar.md")
	if len(args) > 0 {
		target = args[0]
	}
	abs, err := filepath.Abs(expandTilde(target))
	if err != nil {
		fmt.Fprintln(os.Stderr, "sidecar init:", err)
		return 1
	}

	migrated := false
	if isDefaultTarget && !keepBoard {
		// Migrate against the directory the target itself resolves in — the
		// target's parent's parent (.sidecar/sidecar.md → cwd) — not the git
		// root. They coincide in the normal case; from a subdirectory of a
		// repo, a root-level SIDECAR.md is left alone rather than migrated
		// out from under the directory the user actually asked to init.
		root := filepath.Dir(filepath.Dir(abs))
		migrated = migrateLegacyBoard(root, true) // silent — no prompt on bare init
	}

	sections := defaultSections()
	if _, err := os.Stat(abs); err == nil {
		if !migrated {
			fmt.Printf("%s already exists — leaving it untouched.\n", target)
		}
		// Re-runs (migration or otherwise) must reflect the board's actual
		// sections, not silently fall back to the default five and clobber
		// a correct note/hook on rewrite-in-place.
		if raw, rerr := os.ReadFile(abs); rerr == nil {
			if parsed, ok := sectionsFromBoard(string(raw)); ok {
				sections = parsed
			}
		} else if !os.IsNotExist(rerr) {
			// abs just Stat'd successfully, so this is something like
			// EACCES, not a race — falling back to the default five
			// silently would be surprising; say so.
			fmt.Fprintln(os.Stderr, "sidecar init: could not read", target, "—", rerr)
		}
	} else {
		// The picker is the one prompt that survives: it only runs when
		// creating a brand-new board and both stdin and stdout are a
		// terminal, and Ctrl-C there still cancels the whole init.
		if interactiveTTY() {
			picked, interrupted := pickSections(defaultSections())
			if interrupted {
				fmt.Fprintln(os.Stderr, "sidecar init: canceled — nothing written.")
				return 1
			}
			sections = picked
		}
		if err := scaffold(abs, sections); err != nil {
			fmt.Fprintln(os.Stderr, "sidecar init:", err)
			return 1
		}
		fmt.Printf("Created %s\n", target)
	}

	if filepath.Base(filepath.Dir(abs)) == sidecarDirName {
		excludeSidecarDir(repoRootForBoard(abs), true)
	} else {
		gitExcludeDefault(abs)
	}
	if !noClaude {
		root := repoRootForBoard(abs)
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			rel = filepath.Base(abs)
		}
		writeClaudeNote(root, rel, sections)
		writeReconcileHook(root, rel, sections)
	}

	if isDefaultTarget {
		fmt.Println("\nWatch it:  sidecar")
	} else {
		fmt.Printf("\nWatch it:  sidecar %s\n", target)
	}
	return 0
}

// sectionsFromBoard derives Section values from an existing board's own "## "
// headings, so rewriting the CLAUDE.md note or hook fallback in place
// reflects the board's actual sections instead of assuming the default five.
// ok is false when the board doesn't parse (no headings) — callers then fall
// back to defaultSections().
func sectionsFromBoard(raw string) ([]Section, bool) {
	b, ok := parseBoard(raw)
	if !ok {
		return nil, false
	}
	// Index the built-in five by their rendered label so a section that
	// matches one exactly gets its Hint back — sectionFromLabel can't
	// recover a Hint from the heading text alone, and without this every
	// re-run of init over a default (or default-derived) board silently
	// drops the "— hint" lines from the CLAUDE.md note.
	knownHints := map[string]string{}
	for _, s := range defaultSections() {
		knownHints[s.label()] = s.Hint
	}
	sections := make([]Section, len(b.Sections))
	for i, s := range b.Sections {
		sec := sectionFromLabel(s.Label)
		sec.Hint = knownHints[s.Label]
		sections[i] = sec
	}
	return sections, true
}

// sectionFromLabel splits a heading label ("🔥 Hot", "Todo") into a Section:
// the first field becomes Emoji when it looks like an emoji, the rest (or
// the whole label, when it doesn't) becomes Name. No Hint — that's not
// recoverable from the rendered heading.
func sectionFromLabel(label string) Section {
	if emoji, ok := leadingEmoji(label); ok {
		fields := strings.Fields(label)
		return Section{Emoji: emoji, Name: strings.Join(fields[1:], " ")}
	}
	return Section{Name: label}
}

// migrateLegacyBoard moves a root-level SIDECAR.md into .sidecar/sidecar.md
// and untracks it when git knows it. assumeYes skips the prompt. Returns
// true when a move happened.
func migrateLegacyBoard(root string, assumeYes bool) bool {
	legacy := filepath.Join(root, legacyFile)
	target := filepath.Join(root, sidecarDirName, "sidecar.md")
	if _, err := os.Stat(legacy); err != nil {
		return false
	}
	if _, err := os.Stat(target); err == nil {
		return false // new home already populated — leave both alone
	}
	targetRel := filepath.Join(sidecarDirName, "sidecar.md")
	if rel, err := filepath.Rel(root, target); err == nil {
		targetRel = rel
	}
	if !assumeYes {
		if !stdinIsTerminal() {
			// No one can answer — EOF on a piped/hookish stdin makes
			// readChoice() return "", which defaults to yes and would
			// silently rename a file no one agreed to move.
			return false
		}
		fmt.Printf("Move %s into %s/? [Y/n]: ", legacyFile, sidecarDirName)
		if c := readChoice(); c == "n" || c == "no" {
			fmt.Printf("Left %s in place — %s will take precedence once you create it.\n", legacyFile, targetRel)
			return false
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "sidecar init:", err)
		return false
	}
	if err := os.Rename(legacy, target); err != nil {
		fmt.Fprintln(os.Stderr, "sidecar init:", err)
		return false
	}
	if _, tracked := git(root, "ls-files", "--error-unmatch", legacyFile); tracked {
		git(root, "rm", "--cached", "--quiet", legacyFile)
		fmt.Printf("Moved %s to %s and untracked it — commit the deletion when ready.\n", legacyFile, targetRel)
	} else {
		fmt.Printf("Moved %s to %s.\n", legacyFile, targetRel)
	}
	return true
}

// interactiveTTY reports whether both stdin and stdout are terminals — the
// condition for running the full-screen picker. (The plain readChoice prompts
// only need stdin.)
func interactiveTTY() bool {
	return stdinIsTerminal() && term.IsTerminal(int(os.Stdout.Fd()))
}

const claudeNoteMarker = "sidecar:review-queue"
const claudeNoteEndMarker = "<!-- /sidecar:review-queue -->"

// claudeNote is the instruction appended to CLAUDE.md so Claude Code
// sessions in the repo keep the queue updated — and know how to install and
// launch sidecar. rel is the file path relative to the repo root.
func claudeNote(rel string, sections []Section) string {
	var secLines strings.Builder
	for _, s := range sections {
		secLines.WriteString("- `" + s.Header() + "`")
		if s.Hint != "" {
			secLines.WriteString(" — " + s.Hint)
		}
		secLines.WriteString("\n")
	}
	const tmpl = "<!-- sidecar:review-queue -->\n" +
		"## Sidecar board\n\n" +
		"Maintain `%[1]s` — the live board the human watches with `sidecar`.\n" +
		"Move each item to the section that matches its state:\n\n" +
		"%[2]s" +
		"\nWrite entries in Apple Developer documentation voice: declarative,\n" +
		"front-loaded verb, present tense, one fact per sentence. State outcomes,\n" +
		"not process.\n\n" +
		"One entry is at most:\n" +
		"- a status tag and title on the first line\n" +
		"- two sentences of detail — more belongs in the PR or issue you link\n" +
		"- bare URLs, each on its own line\n" +
		"- one `Next:` line naming the single next action (optional)\n\n" +
		"If sidecar isn't installed: `go install github.com/than/sidecar@latest`,\n" +
		"or a prebuilt binary from https://github.com/than/sidecar/releases/latest\n" +
		"<!-- /sidecar:review-queue -->\n"
	return fmt.Sprintf(tmpl, rel, secLines.String())
}

// replaceClaudeNote swaps the content between the sidecar markers for note.
// replaced is false when the file has no complete marker pair.
func replaceClaudeNote(existing, note string) (string, bool) {
	start := strings.Index(existing, "<!-- "+claudeNoteMarker+" -->")
	if start < 0 {
		return existing, false
	}
	end := strings.Index(existing[start:], claudeNoteEndMarker)
	if end < 0 {
		return existing, false
	}
	end = start + end + len(claudeNoteEndMarker)
	return existing[:start] + strings.TrimSuffix(note, "\n") + existing[end:], true
}

// offerClaudeHook asks whether to wire the queue into Claude Code — a
// CLAUDE.md note (the model authors the queue) and optionally a per-turn
// reconcile hook. Default is no, since it edits committed files. Interactive
// only.
func offerClaudeHook(fileAbs string, sections []Section) {
	if !stdinIsTerminal() {
		return
	}
	root := repoRootForBoard(fileAbs)
	rel, err := filepath.Rel(root, fileAbs)
	if err != nil {
		rel = filepath.Base(fileAbs)
	}

	fmt.Print(`
Help Claude keep this board updated?
  [b] CLAUDE.md note + per-turn diff hook (recommended)
  [c] CLAUDE.md note only
  [n] no
Choice [B/c/n]: `)
	switch readChoice() {
	case "c":
		writeClaudeNote(root, rel, sections)
	case "n":
		return
	default: // Enter or "b" — the hook is the product
		writeClaudeNote(root, rel, sections)
		writeReconcileHook(root, rel, sections)
	}
}

func writeClaudeNote(root, rel string, sections []Section) {
	path := filepath.Join(root, "CLAUDE.md")
	if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), claudeNoteMarker) {
		if updated, ok := replaceClaudeNote(string(data), claudeNote(rel, sections)); ok {
			if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
				fmt.Fprintln(os.Stderr, "sidecar init:", err)
				return
			}
			fmt.Println("Updated the sidecar note in CLAUDE.md")
			return
		}
		fmt.Println("CLAUDE.md has a sidecar marker but no closing marker — update it by hand.")
		return
	}
	prefix := ""
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		if !strings.HasSuffix(string(data), "\n") {
			prefix = "\n\n"
		} else if !strings.HasSuffix(string(data), "\n\n") {
			prefix = "\n"
		}
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sidecar init:", err)
		return
	}
	defer f.Close()
	if _, err := f.WriteString(prefix + claudeNote(rel, sections)); err != nil {
		fmt.Fprintln(os.Stderr, "sidecar init:", err)
		return
	}
	fmt.Println("Added a sidecar note to CLAUDE.md")
}

// hookSentinel is a phrase embedded in the reconcile reminder so a re-run of
// `sidecar init` can find and replace a prior sidecar hook — including an
// older SessionStart one — instead of stacking duplicates.
const hookSentinel = "the sidecar review queue"

// reconcileMessageLabels renders the per-turn reminder from plain heading
// labels. reconcileMessage adapts []Section to it. An unparseable board
// yields no labels — the "Sections: …" clause is dropped entirely rather
// than rendering the empty-list degenerate "Sections: .", while the sentinel
// phrase stays intact either way.
//
// The watch-it invocation is bare `sidecar` for the default board path
// (.sidecar/sidecar.md) and `sidecar <rel>` for a custom one — matching how
// init's own closing "Watch it:" hint addresses each case.
func reconcileMessageLabels(rel string, labels []string) string {
	invocation := "sidecar " + rel
	if rel == filepath.Join(sidecarDirName, "sidecar.md") {
		invocation = "sidecar"
	}
	msg := fmt.Sprintf("If your last turn changed task state, reconcile %s — %s the human watches with `%s`.", rel, hookSentinel, invocation)
	if len(labels) == 0 {
		return msg
	}
	return msg + fmt.Sprintf(" Sections: %s.", strings.Join(labels, " / "))
}

// reconcileMessage is the per-turn reminder the hook echoes. It's conditional
// ("if your last turn changed task state") so it costs almost nothing on
// turns that don't touch the queue.
func reconcileMessage(rel string, sections []Section) string {
	labels := make([]string, len(sections))
	for i, s := range sections {
		labels[i] = s.label()
	}
	return reconcileMessageLabels(rel, labels)
}

// reconcileHookEntry runs `sidecar diff` when the binary is installed and
// falls back to the static reminder otherwise — the reminder keeps the
// sentinel phrase, so re-running init still finds and upgrades this hook.
func reconcileHookEntry(rel string, sections []Section) map[string]any {
	diffCmd := "sidecar diff"
	if rel != filepath.Join(sidecarDirName, "sidecar.md") {
		diffCmd += " " + shSingleQuote(rel)
	}
	// `command -v sidecar` only proves a binary named sidecar exists, not
	// that it has the diff subcommand — an older sidecar falls into viewer
	// mode on `sidecar diff` (treating "diff" as a board path) and can hang
	// a TTY-inheriting hook. Probe the actual feature instead: `--help`
	// exits 0 fast on a binary that has it, and </dev/null forces an old
	// binary's viewer-mode fallback to fail fast on stdin rather than hang.
	probe := "sidecar diff --help </dev/null >/dev/null 2>&1"
	cmd := probe + " && " + diffCmd + " || echo " + shSingleQuote(reconcileMessage(rel, sections))
	return map[string]any{
		"hooks": []any{
			map[string]any{"type": "command", "command": cmd},
		},
	}
}

// writeReconcileHook installs a UserPromptSubmit hook — the only hook type
// that fires every turn, so the queue actually stays current. It merges into
// an existing .claude/settings.json, replacing any prior sidecar hook
// (including an older SessionStart one) so re-running `sidecar init` upgrades
// cleanly. If the file exists but isn't valid JSON or has a shape it can't
// safely edit, it prints the snippet instead of risking a clobber.
func writeReconcileHook(root, rel string, sections []Section) {
	path := filepath.Join(root, ".claude", "settings.json")
	entry := reconcileHookEntry(rel, sections)

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		settings := map[string]any{"hooks": map[string]any{"UserPromptSubmit": []any{entry}}}
		if err := writeSettings(path, settings); err != nil {
			fmt.Fprintln(os.Stderr, "sidecar init:", err)
			return
		}
		fmt.Println("Wrote .claude/settings.json with a per-turn reconcile hook.")
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "sidecar init:", err)
		return
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil || settings == nil {
		fmt.Printf(".claude/settings.json isn't valid JSON — add this hook yourself:\n%s\n", snippetJSON(entry))
		return
	}
	if !mergeReconcileHook(settings, entry) {
		fmt.Printf(".claude/settings.json has an unexpected shape — add this hook yourself:\n%s\n", snippetJSON(entry))
		return
	}
	if err := writeSettings(path, settings); err != nil {
		fmt.Fprintln(os.Stderr, "sidecar init:", err)
		return
	}
	fmt.Println("Updated .claude/settings.json with a per-turn reconcile hook.")
}

// mergeReconcileHook strips any prior sidecar-owned hook entries from every
// event array, then appends entry under UserPromptSubmit. It mutates settings
// in place and returns false if settings has a "hooks" value it can't safely
// edit (caller then prints a snippet rather than clobber the file).
func mergeReconcileHook(settings, entry map[string]any) bool {
	hooksAny, ok := settings["hooks"]
	if !ok {
		settings["hooks"] = map[string]any{"UserPromptSubmit": []any{entry}}
		return true
	}
	hooks, ok := hooksAny.(map[string]any)
	if !ok {
		return false
	}
	// Remove prior sidecar hooks across every event (upgrades old SessionStart).
	for event, arrAny := range hooks {
		arr, ok := arrAny.([]any)
		if !ok {
			continue
		}
		kept := make([]any, 0, len(arr))
		for _, e := range arr {
			if !isSidecarHook(e) {
				kept = append(kept, e)
			}
		}
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
	}
	ups, _ := hooks["UserPromptSubmit"].([]any)
	hooks["UserPromptSubmit"] = append(ups, entry)
	return true
}

// isSidecarHook reports whether a hook entry is one sidecar wrote, detected by
// the sentinel phrase in its command string.
func isSidecarHook(entry any) bool {
	m, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	inner, ok := m["hooks"].([]any)
	if !ok {
		return false
	}
	for _, h := range inner {
		hm, ok := h.(map[string]any)
		if !ok {
			continue
		}
		if cmd, ok := hm["command"].(string); ok && strings.Contains(cmd, hookSentinel) {
			return true
		}
	}
	return false
}

// writeSettings marshals settings to path, creating parent dirs as needed.
func writeSettings(path string, settings map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// snippetJSON renders just the hook fragment for the user to paste when we
// won't touch their settings file.
func snippetJSON(entry map[string]any) string {
	frag := map[string]any{"hooks": map[string]any{"UserPromptSubmit": []any{entry}}}
	b, _ := json.MarshalIndent(frag, "", "  ")
	return string(b)
}

// shSingleQuote wraps s in single quotes for a POSIX shell, escaping any
// embedded single quotes.
func shSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// scaffold writes the starter template for the chosen sections, leaving any
// existing file untouched.
func scaffold(abs string, sections []Section) error {
	if _, err := os.Stat(abs); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return os.WriteFile(abs, []byte(renderTemplate(sections)), 0o644)
}

// offerCreate is the interactive prompt shown when the viewer is launched on
// a file that doesn't exist yet and stdin is a terminal. On accept it
// scaffolds the file and offers to git-exclude it; on decline the viewer
// opens on its waiting screen. Non-interactive (piped) stdin skips straight
// to waiting, so scripts aren't blocked.
func offerCreate(abs string) {
	if _, err := os.Stat(abs); err == nil {
		return // already there
	}
	if !stdinIsTerminal() {
		return
	}
	fmt.Printf("%s doesn't exist yet. Create it? [Y/n]: ", filepath.Base(abs))
	switch readChoice() {
	case "n", "no":
		return
	default: // Enter or "y" → create
		sections := defaultSections()
		if interactiveTTY() {
			picked, interrupted := pickSections(defaultSections())
			if interrupted {
				return
			}
			sections = picked
		}
		if err := scaffold(abs, sections); err != nil {
			fmt.Fprintln(os.Stderr, "sidecar:", err)
			return
		}
		fmt.Printf("Created %s\n", filepath.Base(abs))
		offerGitExclude(abs)
		offerClaudeHook(abs, sections)
	}
}

// stdinIsTerminal reports whether stdin is an interactive terminal (not a
// pipe or file), without pulling in a dependency.
func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// excludeSidecarDir appends ".sidecar/" to the repo's .git/info/exclude —
// local and uncommitted, so the repo never learns sidecar exists. No-op
// outside a work tree or when the entry is already ignored. verbose controls
// whether the "Added …" confirmation prints to stdout — init's paths want
// it, but the diff hook's silent seed path (writeSnapshot) must not print
// anything on a plain `sidecar diff` run.
func excludeSidecarDir(dir string, verbose bool) {
	if out, ok := git(dir, "rev-parse", "--is-inside-work-tree"); !ok || out != "true" {
		return
	}
	if _, ignored := git(dir, "check-ignore", "-q", filepath.Join(dir, sidecarDirName)); ignored {
		return
	}
	path, ok := git(dir, "rev-parse", "--git-path", "info/exclude")
	if !ok {
		return
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	writeIgnore(path, sidecarDirName+"/", verbose)
}

// offerGitExclude prompts to keep the file out of git, when inside a work
// tree and the file isn't already ignored. All git state is resolved via
// `git` itself, so linked worktrees and submodules point at the correct
// shared exclude file.
func offerGitExclude(fileAbs string) {
	dir := filepath.Dir(fileAbs)
	if filepath.Base(dir) == sidecarDirName {
		// A board resident inside .sidecar/ (custom name or not) belongs to
		// the same automatic whole-dir exclude as the default board — never
		// the custom-path prompt, which would exclude just the one file and
		// leave the rest of .sidecar/ (including the snapshot) untracked.
		excludeSidecarDir(repoRootForBoard(fileAbs), true)
		return
	}
	if out, ok := git(dir, "rev-parse", "--is-inside-work-tree"); !ok || out != "true" {
		return // not a git work tree — nothing to exclude
	}
	root, ok := git(dir, "rev-parse", "--show-toplevel")
	if !ok {
		return
	}
	rel, err := filepath.Rel(root, fileAbs)
	if err != nil {
		return
	}

	if _, ignored := git(dir, "check-ignore", "-q", fileAbs); ignored {
		fmt.Printf("%s is already git-ignored.\n", rel)
		return
	}

	fmt.Printf(`
Keep %s out of git? (personal scratchpad, not project source)
  [e] .git/info/exclude  — uncommitted; ignored in every worktree (recommended)
  [g] .gitignore         — committed; applies to everyone who clones
  [n] no, leave it tracked
Choice [E/g/n]: `, rel)

	switch readChoice() {
	case "g":
		gitignore := filepath.Join(root, ".gitignore")
		writeIgnore(gitignore, rel, true)
		excludeCustomPathSnapshotDirTo(gitignore, rel)
	case "n":
		fmt.Println("Left tracked.")
	default: // "e" or Enter → recommended
		applyExcludeDefault(dir, rel)
		excludeCustomPathSnapshotDir(dir, rel)
	}
}

// gitExcludeDefault takes offerGitExclude's recommended default — appending
// the file to .git/info/exclude — without printing a prompt or reading
// stdin. Used by `sidecar init --yes`, which decides by flag rather than
// TTY state. No-op outside a work tree or when the file is already ignored.
func gitExcludeDefault(fileAbs string) {
	dir := filepath.Dir(fileAbs)
	if filepath.Base(dir) == sidecarDirName {
		// Same automatic whole-dir exclude as offerGitExclude — see there.
		excludeSidecarDir(repoRootForBoard(fileAbs), true)
		return
	}
	if out, ok := git(dir, "rev-parse", "--is-inside-work-tree"); !ok || out != "true" {
		return // not a git work tree — nothing to exclude
	}
	root, ok := git(dir, "rev-parse", "--show-toplevel")
	if !ok {
		return
	}
	rel, err := filepath.Rel(root, fileAbs)
	if err != nil {
		return
	}
	if _, ignored := git(dir, "check-ignore", "-q", fileAbs); ignored {
		fmt.Printf("%s is already git-ignored.\n", rel)
		return
	}
	applyExcludeDefault(dir, rel)
	excludeCustomPathSnapshotDir(dir, rel)
}

// applyExcludeDefault appends rel to dir's .git/info/exclude — the shared
// action behind both the interactive "recommended" choice and the
// non-interactive --yes default.
func applyExcludeDefault(dir, rel string) {
	path, ok := git(dir, "rev-parse", "--git-path", "info/exclude")
	if !ok {
		fmt.Fprintln(os.Stderr, "could not locate .git/info/exclude")
		return
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	writeIgnore(path, rel, true)
}

// excludeCustomPathSnapshotDir excludes the .sidecar/ directory that will
// appear beside a custom board path once `sidecar diff` writes its snapshot
// there (previous-<hash>.md) — otherwise it shows up in git status even
// though the board file itself is ignored. rel is the board's path relative
// to the repo root that dir's exclude file governs; idempotent via
// appendLine.
func excludeCustomPathSnapshotDir(dir, rel string) {
	sidecarRel := filepath.Join(filepath.Dir(rel), sidecarDirName) + "/"
	applyExcludeDefault(dir, sidecarRel)
}

// excludeCustomPathSnapshotDirTo is excludeCustomPathSnapshotDir's
// counterpart for callers that already know the target ignore file (e.g. a
// committed .gitignore from the [g] choice) rather than resolving
// .git/info/exclude via git. Idempotent via writeIgnore/appendLine.
func excludeCustomPathSnapshotDirTo(ignoreFile, rel string) {
	sidecarRel := filepath.Join(filepath.Dir(rel), sidecarDirName) + "/"
	writeIgnore(ignoreFile, sidecarRel, true)
}

// writeIgnore appends line to the ignore file at path, printing a
// confirmation to stdout when verbose — callers on a silent path (a hook's
// snapshot write, see excludeSidecarDir) pass false so stdout stays empty;
// errors still go to stderr either way.
func writeIgnore(path, line string, verbose bool) {
	if err := appendLine(path, line); err != nil {
		fmt.Fprintln(os.Stderr, "sidecar init:", err)
		return
	}
	if !verbose {
		return
	}
	// Show a repo-relative-ish label for the ignore file.
	label := path
	if wd, err := os.Getwd(); err == nil {
		if r, err := filepath.Rel(wd, path); err == nil && !strings.HasPrefix(r, "..") {
			label = r
		}
	}
	fmt.Printf("Added %q to %s\n", line, label)
}

// repoRoot resolves the git work tree root for dir via `git rev-parse
// --show-toplevel`, falling back to dir itself outside a work tree (or when
// git is missing) — the shared lookup behind every call site that needs a
// path relative to the repo root but must still work outside a repo.
func repoRoot(dir string) string {
	if r, ok := git(dir, "rev-parse", "--show-toplevel"); ok {
		return r
	}
	return dir
}

// repoRootForBoard resolves the root that should own a board's CLAUDE.md
// note and hook. It's the board's own parent directory — except when that
// parent is .sidecar/ (the default board's home), where it steps up one
// more level first. Outside a git work tree repoRoot has no toplevel to
// override the fallback, so without that step-up a default board
// (.sidecar/sidecar.md) would seed CLAUDE.md and .claude/settings.json
// *inside* .sidecar/ and compute rel as the bare "sidecar.md" — a path that
// doesn't exist at the resulting (wrong) root, breaking the hook. Same
// reasoning offerGitExclude/gitExcludeDefault already use for the exclude
// path.
func repoRootForBoard(boardAbs string) string {
	dir := filepath.Dir(boardAbs)
	if filepath.Base(dir) == sidecarDirName {
		dir = filepath.Dir(dir)
	}
	return repoRoot(dir)
}

// git runs a git command in dir and returns trimmed stdout; ok is false if
// git is missing or exits non-zero.
func git(dir string, args ...string) (out string, ok bool) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	b, err := cmd.Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

func readChoice() string {
	sc := bufio.NewScanner(os.Stdin)
	if sc.Scan() {
		return strings.ToLower(strings.TrimSpace(sc.Text()))
	}
	return ""
}

// appendLine appends line to the file (creating it and parent dirs), unless
// an identical line is already present. It preserves a trailing newline.
func appendLine(path, line string) error {
	needLeadingNL := false
	if data, err := os.ReadFile(path); err == nil {
		for _, l := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(l) == line {
				return nil // already there
			}
		}
		needLeadingNL = len(data) > 0 && !strings.HasSuffix(string(data), "\n")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	prefix := ""
	if needLeadingNL {
		prefix = "\n"
	}
	_, err = f.WriteString(prefix + line + "\n")
	return err
}
