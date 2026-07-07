package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const defaultFile = "~/Broomfitters/house/REVIEW.md"

func main() {
	path := defaultFile
	if len(os.Args) > 1 {
		if os.Args[1] == "-h" || os.Args[1] == "--help" {
			fmt.Printf(`sidecar — live-updating markdown viewer for a terminal side pane

usage: sidecar [file.md]   (default: %s)

keys:  j/k, arrows, PgUp/PgDn, mouse wheel  scroll
       g / G                                top / bottom
       r                                    force reload
       q                                    quit
`, defaultFile)
			return
		}
		path = os.Args[1]
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
