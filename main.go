package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const usage = `sidecar — live-updating markdown viewer for a terminal side pane

usage: sidecar <file.md>

keys:  j/k, arrows, PgUp/PgDn, mouse wheel  scroll
       g / G                                top / bottom
       r                                    force reload
       q                                    quit

The file doesn't have to exist yet — sidecar waits for it and renders the
moment it appears, then live-reloads on every change.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if os.Args[1] == "-h" || os.Args[1] == "--help" {
		fmt.Print(usage)
		return
	}
	path := os.Args[1]

	abs, err := filepath.Abs(expandTilde(path))
	if err != nil {
		fmt.Fprintln(os.Stderr, "sidecar:", err)
		os.Exit(1)
	}

	p := tea.NewProgram(newModel(abs),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	go watchFile(abs, p.Send)

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "sidecar:", err)
		os.Exit(1)
	}
}

func expandTilde(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
