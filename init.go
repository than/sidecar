package main

import (
	"bufio"
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

## 🔴 Needs action

- nothing yet

## 🟡 In progress

- nothing yet

## 🟢 Done

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
	} else {
		if err := os.WriteFile(abs, []byte(starterTemplate), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "sidecar init:", err)
			return 1
		}
		fmt.Printf("Created %s\n", target)
	}

	offerGitExclude(abs)

	fmt.Printf("\nWatch it:  sidecar %s\n", filepath.Base(abs))
	return 0
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
  [e] .git/info/exclude  — local only, shared across worktrees (recommended)
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
