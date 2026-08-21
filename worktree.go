// worktree.go — git worktree awareness for board resolution. A monorepo
// with one worktree per app gives every worktree its own cwd, and the board
// path is relative to cwd, so each one quietly gets its own board: the human
// watches one file while an agent diligently maintains another, and both
// experience it as the other side not doing their job.
//
// Everything here reads the filesystem directly rather than shelling out to
// git. `sidecar diff` runs from a hook on every agent turn, so a subprocess
// per turn is a real cost, and the plain-file layout is stable enough to
// read: a linked worktree's .git is a file, not a directory.
package main

import (
	"os"
	"path/filepath"
	"strings"
)

// mainCheckout returns the working tree of the main checkout when dir is a
// linked git worktree, and "" otherwise — including for the main checkout
// itself, a plain clone, and anything outside a repository.
//
// A linked worktree's .git is a file reading `gitdir: /main/.git/worktrees/x`.
// Trimming the /worktrees/x tail gives the shared git dir; its parent is the
// main checkout.
func mainCheckout(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, ".git"))
	if err != nil {
		return "" // a directory (main checkout) or no repo at all
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), "gitdir:"))
	if gitdir == "" {
		return ""
	}
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(dir, gitdir)
	}
	i := strings.LastIndex(filepath.ToSlash(filepath.Clean(gitdir)), "/worktrees/")
	if i < 0 {
		return ""
	}
	commonDir := filepath.FromSlash(filepath.ToSlash(filepath.Clean(gitdir))[:i])
	root := filepath.Dir(commonDir)
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return ""
	}
	return root
}

// worktreeRoots returns the working trees linked to the checkout at root,
// not including root itself. Each .git/worktrees/<name>/gitdir names that
// worktree's .git file; its parent directory is the worktree.
func worktreeRoots(root string) []string {
	entries, err := os.ReadDir(filepath.Join(root, ".git", "worktrees"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(root, ".git", "worktrees", e.Name(), "gitdir"))
		if err != nil {
			continue
		}
		p := strings.TrimSpace(string(data))
		if p == "" {
			continue
		}
		out = append(out, filepath.Dir(filepath.Clean(p)))
	}
	return out
}

// boardIn reports the standard board inside dir when it exists.
func boardIn(dir string) string {
	p := filepath.Join(dir, sidecarDirName, "sidecar.md")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// sharedBoard returns the main checkout's board when cwd is a linked
// worktree that has no board of its own. A worktree is a place to do the
// work, not a separate project, so the shared board is what one human
// watching one pane actually wants.
func sharedBoard() string {
	root := mainCheckout(".")
	if root == "" {
		return ""
	}
	return boardIn(root)
}

// splitBoards returns the other boards in the same repository as abs — the
// main checkout's and every linked worktree's — excluding abs itself. A
// non-empty result means two boards are live at once and whichever one the
// human is watching is silently partial.
func splitBoards(abs string) []string {
	dir := filepath.Dir(filepath.Dir(abs)) // strip /.sidecar/sidecar.md
	root := mainCheckout(dir)
	if root == "" {
		root = dir
	}
	var out []string
	for _, d := range append([]string{root}, worktreeRoots(root)...) {
		b := boardIn(d)
		if b == "" {
			continue
		}
		if same, err := filepath.EvalSymlinks(b); err == nil {
			if realAbs, err := filepath.EvalSymlinks(abs); err == nil && same == realAbs {
				continue
			}
		}
		if b != abs {
			out = append(out, b)
		}
	}
	return out
}
