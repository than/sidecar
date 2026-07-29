package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runInit scaffolds the target file and offers to keep it out of git.
// Returns a process exit code.
func runInit(args []string) int {
	target := defaultFile
	if len(args) > 0 {
		target = args[0]
	}
	abs, err := filepath.Abs(expandTilde(target))
	if err != nil {
		fmt.Fprintln(os.Stderr, "sidecar init:", err)
		return 1
	}

	sections := defaultSections()
	if _, err := os.Stat(abs); err == nil {
		fmt.Printf("%s already exists — leaving it untouched.\n", target)
	} else {
		if stdinIsTerminal() {
			sections = pickSections(defaultSections())
		}
		if err := scaffold(abs, sections); err != nil {
			fmt.Fprintln(os.Stderr, "sidecar init:", err)
			return 1
		}
		fmt.Printf("Created %s\n", target)
	}

	offerGitExclude(abs)
	offerClaudeHook(abs, sections)

	fmt.Printf("\nWatch it:  sidecar %s\n", filepath.Base(abs))
	return 0
}

const claudeNoteMarker = "sidecar:review-queue"

// claudeNote is the instruction appended to CLAUDE.md so Claude Code
// sessions in the repo keep the queue updated — and know how to install and
// launch sidecar. rel is the file path relative to the repo root.
func claudeNote(rel string, sections []Section) string {
	var secLines strings.Builder
	for _, s := range sections {
		secLines.WriteString("`" + s.Header() + "`")
		if s.Hint != "" {
			secLines.WriteString(" — " + s.Hint)
		}
		secLines.WriteString("\n")
	}
	const tmpl = "<!-- sidecar:review-queue -->\n" +
		"## Review queue (sidecar)\n\n" +
		"Maintain `%[1]s` as a live review / TODO queue for the human. Sections:\n" +
		"%[2]s" +
		"Put bare URLs on their own line (keeps them clickable); keep entries short.\n\n" +
		"The human watches it live with `sidecar %[1]s`. If sidecar isn't installed:\n" +
		"`go install github.com/than/sidecar@latest`, or a prebuilt binary from\n" +
		"https://github.com/than/sidecar/releases/latest\n" +
		"<!-- /sidecar:review-queue -->\n"
	return fmt.Sprintf(tmpl, rel, secLines.String())
}

// offerClaudeHook asks whether to wire the queue into Claude Code — a
// CLAUDE.md note (the model authors the queue) and optionally a per-turn
// reconcile hook. Default is no, since it edits committed files. Interactive
// only.
func offerClaudeHook(fileAbs string, sections []Section) {
	if !stdinIsTerminal() {
		return
	}
	dir := filepath.Dir(fileAbs)
	root := dir
	if r, ok := git(dir, "rev-parse", "--show-toplevel"); ok {
		root = r
	}
	rel, err := filepath.Rel(root, fileAbs)
	if err != nil {
		rel = filepath.Base(fileAbs)
	}

	fmt.Print(`
Help Claude keep this queue updated? (adds an instruction for Claude Code)
  [c] CLAUDE.md note only
  [b] CLAUDE.md note + per-turn UserPromptSubmit reconcile hook (recommended)
  [n] no
Choice [c/b/N]: `)
	switch readChoice() {
	case "c":
		writeClaudeNote(root, rel, sections)
	case "b":
		writeClaudeNote(root, rel, sections)
		writeReconcileHook(root, rel, sections)
	default:
		return
	}
}

func writeClaudeNote(root, rel string, sections []Section) {
	path := filepath.Join(root, "CLAUDE.md")
	if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), claudeNoteMarker) {
		fmt.Println("CLAUDE.md already has the sidecar note.")
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

// reconcileMessage is the per-turn reminder the hook echoes. It's conditional
// ("if your last turn changed task state") so it costs almost nothing on
// turns that don't touch the queue.
func reconcileMessage(rel string, sections []Section) string {
	labels := make([]string, len(sections))
	for i, s := range sections {
		labels[i] = s.label()
	}
	return fmt.Sprintf("If your last turn changed task state, reconcile %s — %s the human watches with `sidecar %s`. Sections: %s.", rel, hookSentinel, rel, strings.Join(labels, " / "))
}

// reconcileHookEntry is a single Claude Code hook entry (one matcher, one
// command) that echoes the reminder.
func reconcileHookEntry(rel string, sections []Section) map[string]any {
	return map[string]any{
		"hooks": []any{
			map[string]any{"type": "command", "command": "echo " + shSingleQuote(reconcileMessage(rel, sections))},
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
		if stdinIsTerminal() {
			sections = pickSections(defaultSections())
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

// offerGitExclude prompts to keep the file out of git, when inside a work
// tree and the file isn't already ignored. All git state is resolved via
// `git` itself, so linked worktrees and submodules point at the correct
// shared exclude file.
func offerGitExclude(fileAbs string) {
	dir := filepath.Dir(fileAbs)
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
		writeIgnore(filepath.Join(root, ".gitignore"), rel)
	case "n":
		fmt.Println("Left tracked.")
	default: // "e" or Enter → recommended
		path, ok := git(dir, "rev-parse", "--git-path", "info/exclude")
		if !ok {
			fmt.Fprintln(os.Stderr, "could not locate .git/info/exclude")
			return
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		writeIgnore(path, rel)
	}
}

func writeIgnore(path, line string) {
	if err := appendLine(path, line); err != nil {
		fmt.Fprintln(os.Stderr, "sidecar init:", err)
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
