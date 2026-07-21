package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const defaultFile = "SIDECAR.md"

const help = `sidecar — live-updating markdown viewer for a terminal side pane

usage: sidecar [file.md]        (default: ./SIDECAR.md)
       sidecar init [file.md]   scaffold the file, optionally git-exclude it

keys:  j/k, arrows, PgUp/PgDn, mouse wheel  scroll
       g / G                                top / bottom
       r                                    force reload
       q                                    quit

The file doesn't have to exist yet — sidecar waits for it and renders the
moment it appears, then live-reloads on every change.
`

func main() {
	path := defaultFile
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-h", "--help":
			fmt.Print(help)
			return
		case "init":
			os.Exit(runInit(os.Args[2:]))
		default:
			path = os.Args[1]
		}
	}

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
