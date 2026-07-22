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

// starterTemplate is written by `sidecar init` when the target doesn't yet
// exist. Kept generic and format-forward: bare URLs on their own line stay
// clickable, emoji markers scan fast.
const starterTemplate = `# Sidecar

Live scratchpad and review queue — edit this file, or let your agent
maintain it, and sidecar re-renders on every save.

## 🧠 Needs action

- nothing yet

## 🚧 In progress

- nothing yet

## ✅ Done

- nothing yet

## 📦 Shipped

- nothing yet
`

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

	if _, err := os.Stat(abs); err == nil {
		fmt.Printf("%s already exists — leaving it untouched.\n", target)
	} else if err := scaffold(abs); err != nil {
		fmt.Fprintln(os.Stderr, "sidecar init:", err)
		return 1
	} else {
		fmt.Printf("Created %s\n", target)
	}

	offerGitExclude(abs)
	offerClaudeHook(abs)

	fmt.Printf("\nWatch it:  sidecar %s\n", filepath.Base(abs))
	return 0
}

const claudeNoteMarker = "sidecar:review-queue"

// claudeNote is the instruction appended to CLAUDE.md so Claude Code
// sessions in the repo keep the queue updated — and know how to install and
// launch sidecar. rel is the file path relative to the repo root.
func claudeNote(rel string) string {
	const tmpl = "<!-- sidecar:review-queue -->\n" +
		"## Review queue (sidecar)\n\n" +
		"Maintain `%[1]s` as a live review / TODO queue for the human. Sections:\n" +
		"`## 🧠 Needs action`, `## 🚧 In progress`, `## ✅ Done`, `## 📦 Shipped`.\n" +
		"Put bare URLs on their own line (keeps them clickable); keep entries short.\n\n" +
		"The human watches it live with `sidecar %[1]s`. If sidecar isn't installed:\n" +
		"`go install github.com/than/sidecar@latest`, or a prebuilt binary from\n" +
		"https://github.com/than/sidecar/releases/latest\n" +
		"<!-- /sidecar:review-queue -->\n"
	return fmt.Sprintf(tmpl, rel)
}

// offerClaudeHook asks whether to wire the queue into Claude Code — a
// CLAUDE.md note (the model authors the queue) and optionally a SessionStart
// hook. Default is no, since it edits committed files. Interactive only.
func offerClaudeHook(fileAbs string) {
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
  [c] CLAUDE.md note (recommended)
  [b] CLAUDE.md note + a SessionStart hook (.claude/settings.json)
  [n] no
Choice [c/b/N]: `)
	switch readChoice() {
	case "c":
		writeClaudeNote(root, rel)
	case "b":
		writeClaudeNote(root, rel)
		writeSessionHook(root, rel)
	default:
		return
	}
}

func writeClaudeNote(root, rel string) {
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
	if _, err := f.WriteString(prefix + claudeNote(rel)); err != nil {
		fmt.Fprintln(os.Stderr, "sidecar init:", err)
		return
	}
	fmt.Println("Added a sidecar note to CLAUDE.md")
}

// writeSessionHook creates .claude/settings.json with a SessionStart
// reminder when it doesn't exist yet. If the file already exists, it prints
// the snippet instead of merging — never risk clobbering a user's settings.
func writeSessionHook(root, rel string) {
	path := filepath.Join(root, ".claude", "settings.json")
	snippet := sessionHookJSON(rel)
	if _, err := os.Stat(path); err == nil {
		fmt.Printf(".claude/settings.json exists — add this SessionStart hook yourself:\n%s\n", snippet)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "sidecar init:", err)
		return
	}
	if err := os.WriteFile(path, []byte(snippet), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "sidecar init:", err)
		return
	}
	fmt.Println("Wrote .claude/settings.json with a SessionStart reminder.")
}

func sessionHookJSON(rel string) string {
	msg := fmt.Sprintf("Maintain %s as the sidecar review queue; the human watches it with `sidecar %s`. Sections: 🧠 Needs action / 🚧 In progress / ✅ Done / 📦 Shipped.", rel, rel)
	settings := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{"type": "command", "command": "echo " + shSingleQuote(msg)},
					},
				},
			},
		},
	}
	b, _ := json.MarshalIndent(settings, "", "  ")
	return string(b) + "\n"
}

// shSingleQuote wraps s in single quotes for a POSIX shell, escaping any
// embedded single quotes.
func shSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// scaffold writes the starter template, leaving any existing file untouched.
func scaffold(abs string) error {
	if _, err := os.Stat(abs); err == nil {
		return nil
	}
	return os.WriteFile(abs, []byte(starterTemplate), 0o644)
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
		if err := scaffold(abs); err != nil {
			fmt.Fprintln(os.Stderr, "sidecar:", err)
			return
		}
		fmt.Printf("Created %s\n", filepath.Base(abs))
		offerGitExclude(abs)
		offerClaudeHook(abs)
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
