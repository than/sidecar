package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

const defaultFile = "SIDECAR.md"

// version is overwritten at release time via -ldflags -X main.version.
var version = "dev"

const help = `sidecar — live-updating markdown viewer for a terminal side pane

usage: sidecar [file.md]         (default: ./SIDECAR.md)
       sidecar init [file.md]    scaffold the file; offer to git-exclude it
                                 and wire it into Claude Code
       sidecar --static [file]   render once to stdout and exit (no TUI)
       sidecar --no-flash [file] disable the subtle change-flash (▸ still shows)

keys:  j/k, arrows, PgUp/PgDn                scroll
       g / G                                top / bottom
       r                                    force reload
       q                                    quit

The file doesn't have to exist yet — sidecar waits for it and renders the
moment it appears, then live-reloads on every change.
`

func main() {
	// Subcommands dispatch on the first arg, exactly as before.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-h", "--help":
			fmt.Print(help)
			return
		case "-v", "--version":
			fmt.Println("sidecar", version)
			return
		case "-s", "--static":
			os.Exit(runStatic(os.Args[2:]))
		case "init":
			os.Exit(runInit(os.Args[2:]))
		}
	}

	// Viewer mode: an optional file path plus the --no-flash flag, any order.
	path := defaultFile
	noFlash := false
	for _, a := range os.Args[1:] {
		switch {
		case a == "--no-flash":
			noFlash = true
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "sidecar: unknown flag %q\n\n", a)
			fmt.Print(help)
			os.Exit(2)
		default:
			path = a
		}
	}

	abs, err := filepath.Abs(expandTilde(path))
	if err != nil {
		fmt.Fprintln(os.Stderr, "sidecar:", err)
		os.Exit(1)
	}

	offerCreate(abs) // if missing and interactive, offer to scaffold before opening

	p := tea.NewProgram(newModel(abs, noFlash),
		tea.WithAltScreen(),
		// No mouse capture: keeps the terminal's native text selection and
		// clickable links working. Scroll with the keyboard (see keys below).
	)
	go watchFile(abs, p.Send)

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "sidecar:", err)
		os.Exit(1)
	}
}

// runStatic renders the file once to stdout and exits — no watching, no
// alt-screen. Handy for piping, CI, and quick inline checks. Width is the
// terminal width (minus 2) when stdout is a TTY, else 80.
func runStatic(args []string) int {
	path := defaultFile
	if len(args) > 0 {
		path = args[0]
	}
	abs, err := filepath.Abs(expandTilde(path))
	if err != nil {
		fmt.Fprintln(os.Stderr, "sidecar:", err)
		return 1
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sidecar:", err)
		return 1
	}
	width := 80
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		width = w
	}
	out, err := renderMarkdown(string(data), width-2)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sidecar:", err)
		return 1
	}
	fmt.Println(out)
	return 0
}

func expandTilde(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
