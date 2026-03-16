package main

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// ANSI codes matching the notification box design
const (
	pickOrange = "\033[38;5;214m"
	pickBold   = "\033[1;37m"
	pickDim    = "\033[38;5;250m"
	pickReset  = "\033[0m"
)

// runPick runs an interactive picker in the terminal with the same bordered
// box design as the notification panel. Args: --title "Title" --sentinel "path" option1 option2 ...
func runPick(args []string) {
	title := "Choose an option"
	sentinel_path := ""
	var options []string

	// Parse args
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--title":
			if i+1 < len(args) {
				title = args[i+1]
				i++
			}
		case "--sentinel":
			if i+1 < len(args) {
				sentinel_path = args[i+1]
				i++
			}
		case "--width":
			// ignored for now — we detect terminal width
			i++
		default:
			options = append(options, args[i])
		}
	}

	if len(options) == 0 {
		fmt.Fprintln(os.Stderr, "wt _pick: no options provided")
		os.Exit(1)
	}

	// Get terminal width
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width < 10 {
		width = 80
	}

	// Put terminal in raw mode for key reading
	old_state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "wt _pick: failed to set raw mode: %v\n", err)
		os.Exit(1)
	}
	defer term.Restore(int(os.Stdin.Fd()), old_state)

	cursor := 0
	total := len(options)

	draw := func() {
		inner := width - 2
		if inner < 1 {
			inner = 1
		}

		// Move to top, hide cursor
		fmt.Print("\033[H\033[?25l")

		// Top border
		title_fill := inner - 1 - len(title) - 2
		if title_fill < 1 {
			title_fill = 1
		}
		fmt.Printf("%s╭─ %s%s%s %s╮%s\r\n",
			pickOrange, pickBold, title, pickOrange,
			strings.Repeat("─", title_fill), pickReset)

		// Options
		for i, opt := range options {
			vis_len := len(opt) + 4 // "▸ " or "  " prefix
			pad := inner - vis_len
			if pad < 0 {
				pad = 0
			}
			if i == cursor {
				fmt.Printf("%s│%s %s▸ %s%s%s %s│%s\r\n",
					pickOrange, pickReset, pickOrange, pickBold, opt, pickReset,
					strings.Repeat(" ", pad)+pickOrange, pickReset)
			} else {
				fmt.Printf("%s│%s   %s%s%s %s│%s\r\n",
					pickOrange, pickReset, pickDim, opt, pickReset,
					strings.Repeat(" ", pad)+pickOrange, pickReset)
			}
		}

		// Bottom border
		inner2 := width - 2
		if inner2 < 1 {
			inner2 = 1
		}
		fmt.Printf("%s╰%s╯%s",
			pickOrange, strings.Repeat("─", inner2), pickReset)
	}

	draw()

	// Input loop
	buf := make([]byte, 3)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			continue
		}

		switch {
		// Arrow up: ESC [ A
		case n == 3 && buf[0] == 0x1b && buf[1] == '[' && buf[2] == 'A':
			cursor = (cursor - 1 + total) % total
			draw()

		// Arrow down: ESC [ B
		case n == 3 && buf[0] == 0x1b && buf[1] == '[' && buf[2] == 'B':
			cursor = (cursor + 1) % total
			draw()

		// Enter
		case n == 1 && (buf[0] == '\r' || buf[0] == '\n'):
			term.Restore(int(os.Stdin.Fd()), old_state)
			fmt.Print("\033[?25h") // show cursor
			if sentinel_path != "" {
				os.WriteFile(sentinel_path, []byte(options[cursor]+"\n"), 0644)
			} else {
				fmt.Println(options[cursor])
			}
			os.Exit(0)

		// Escape or q — cancel
		case n == 1 && (buf[0] == 0x1b || buf[0] == 'q'):
			term.Restore(int(os.Stdin.Fd()), old_state)
			fmt.Print("\033[?25h")
			if sentinel_path != "" {
				os.WriteFile(sentinel_path, []byte("\n"), 0644)
			}
			os.Exit(0)
		}
	}
}
