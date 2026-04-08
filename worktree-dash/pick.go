package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

// ANSI codes matching the notification box design
const (
	pickOrange = "\033[38;5;214m"
	pickBold   = "\033[1;37m"
	pickDim    = "\033[38;5;250m"
	pickReset  = "\033[0m"
)

// runConfirm shows a yes/no confirmation dialog in the bordered box.
// Args: --title "Title" --prompt "Question?" --sentinel "path"
func runConfirm(args []string) {
	title := "Confirm"
	prompt := "Are you sure?"
	sentinel_path := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--title":
			if i+1 < len(args) {
				title = args[i+1]
				i++
			}
		case "--prompt":
			if i+1 < len(args) {
				prompt = args[i+1]
				i++
			}
		case "--sentinel":
			if i+1 < len(args) {
				sentinel_path = args[i+1]
				i++
			}
		}
	}

	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width < 10 {
		width = 80
	}

	old_state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "wt _confirm: failed to set raw mode: %v\n", err)
		os.Exit(1)
	}
	defer term.Restore(int(os.Stdin.Fd()), old_state)

	cursor := 0 // 0 = Yes, 1 = No
	options := []string{"Yes", "No"}

	draw := func() {
		inner := width - 2
		if inner < 1 {
			inner = 1
		}

		fmt.Print("\033[2J\033[H\033[?25l")

		// Top border
		title_fill := inner - 1 - len(title) - 2
		if title_fill < 1 {
			title_fill = 1
		}
		fmt.Printf("%s╭─ %s%s%s %s╮%s\r\n",
			pickOrange, pickBold, title, pickOrange,
			strings.Repeat("─", title_fill), pickReset)

		// Prompt line
		prompt_pad := inner - len(prompt) - 2
		if prompt_pad < 0 {
			prompt_pad = 0
		}
		fmt.Printf("%s│%s %s%s%s%s │%s\r\n",
			pickOrange, pickReset, pickBold, prompt, pickReset,
			strings.Repeat(" ", prompt_pad)+pickOrange, pickReset)

		// Options line: [Yes]  [No]
		var opt_parts []string
		for i, opt := range options {
			if i == cursor {
				opt_parts = append(opt_parts, fmt.Sprintf("%s▸ %s%s", pickOrange, opt, pickReset))
			} else {
				opt_parts = append(opt_parts, fmt.Sprintf("  %s%s%s", pickDim, opt, pickReset))
			}
		}
		opt_line := strings.Join(opt_parts, "    ")
		opt_vis := 0
		for j, opt := range options {
			opt_vis += len(opt) + 2 // prefix "▸ " or "  "
			if j < len(options)-1 {
				opt_vis += 4 // separator "    "
			}
		}
		opt_pad := inner - opt_vis - 2
		if opt_pad < 0 {
			opt_pad = 0
		}
		fmt.Printf("%s│%s %s%s%s │%s\r\n",
			pickOrange, pickReset, opt_line,
			strings.Repeat(" ", opt_pad), pickOrange, pickReset)

		// Bottom border
		fmt.Printf("%s╰%s╯%s",
			pickOrange, strings.Repeat("─", inner), pickReset)
	}

	write_result := func(result string) {
		term.Restore(int(os.Stdin.Fd()), old_state)
		fmt.Print("\033[?25h")
		if sentinel_path != "" {
			os.WriteFile(sentinel_path, []byte(result+"\n"), 0644)
		}
		os.Exit(0)
	}

	draw()

	buf := make([]byte, 3)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			continue
		}

		switch {
		// Arrow left / Arrow up
		case n == 3 && buf[0] == 0x1b && buf[1] == '[' && (buf[2] == 'D' || buf[2] == 'A'):
			cursor = 0
			draw()

		// Arrow right / Arrow down
		case n == 3 && buf[0] == 0x1b && buf[1] == '[' && (buf[2] == 'C' || buf[2] == 'B'):
			cursor = 1
			draw()

		// Enter
		case n == 1 && (buf[0] == '\r' || buf[0] == '\n'):
			if cursor == 0 {
				write_result("yes")
			} else {
				write_result("")
			}

		// y
		case n == 1 && buf[0] == 'y':
			write_result("yes")

		// n or Escape or q
		case n == 1 && (buf[0] == 'n' || buf[0] == 0x1b || buf[0] == 'q'):
			write_result("")
		}
	}
}

// runInput shows a text input dialog in the bordered box.
// Args: --title "Title" --prompt "Label:" --sentinel "path"
func runInput(args []string) {
	title := "Input"
	prompt := "Value:"
	sentinel_path := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--title":
			if i+1 < len(args) {
				title = args[i+1]
				i++
			}
		case "--prompt":
			if i+1 < len(args) {
				prompt = args[i+1]
				i++
			}
		case "--sentinel":
			if i+1 < len(args) {
				sentinel_path = args[i+1]
				i++
			}
		}
	}

	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width < 10 {
		width = 80
	}

	old_state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "wt _input: failed to set raw mode: %v\n", err)
		os.Exit(1)
	}
	defer term.Restore(int(os.Stdin.Fd()), old_state)

	value := ""

	draw := func() {
		inner := width - 2
		if inner < 1 {
			inner = 1
		}

		fmt.Print("\033[2J\033[H\033[?25l")

		// Top border
		title_fill := inner - 1 - len(title) - 2
		if title_fill < 1 {
			title_fill = 1
		}
		fmt.Printf("%s╭─ %s%s%s %s╮%s\r\n",
			pickOrange, pickBold, title, pickOrange,
			strings.Repeat("─", title_fill), pickReset)

		// Prompt + value line
		display := fmt.Sprintf("%s %s%s█%s", prompt, pickBold, value, pickReset)
		display_vis := len(prompt) + 1 + len(value) + 1 // prompt + space + value + cursor
		pad := inner - display_vis - 2
		if pad < 0 {
			pad = 0
		}
		fmt.Printf("%s│%s %s%s%s │%s\r\n",
			pickOrange, pickReset, display,
			strings.Repeat(" ", pad), pickOrange, pickReset)

		// Bottom border
		fmt.Printf("%s╰%s╯%s",
			pickOrange, strings.Repeat("─", inner), pickReset)
	}

	write_result := func(result string) {
		term.Restore(int(os.Stdin.Fd()), old_state)
		fmt.Print("\033[?25h")
		if sentinel_path != "" {
			os.WriteFile(sentinel_path, []byte(result+"\n"), 0644)
		}
		os.Exit(0)
	}

	draw()

	buf := make([]byte, 4)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			continue
		}

		switch {
		// Enter — submit
		case n == 1 && (buf[0] == '\r' || buf[0] == '\n'):
			write_result(value)

		// Escape — cancel
		case n == 1 && buf[0] == 0x1b:
			write_result("")

		// Ctrl+C — cancel
		case n == 1 && buf[0] == 0x03:
			write_result("")

		// Backspace (0x7f) or Ctrl+H (0x08)
		case n == 1 && (buf[0] == 0x7f || buf[0] == 0x08):
			if len(value) > 0 {
				value = value[:len(value)-1]
				draw()
			}

		// Printable ASCII
		case n == 1 && buf[0] >= 0x20 && buf[0] < 0x7f:
			value += string(buf[0])
			draw()
		}
	}
}

// selectAndExit restores the terminal, writes the selection to the sentinel file, and exits.
// An empty selection means the user cancelled.
func selectAndExit(old_state *term.State, sentinel_path, selection string) {
	term.Restore(int(os.Stdin.Fd()), old_state)
	fmt.Print("\033[?25h")
	if sentinel_path != "" {
		os.WriteFile(sentinel_path, []byte(selection+"\n"), 0644)
	} else if selection != "" {
		fmt.Println(selection)
	}
	os.Exit(0)
}

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
		fmt.Print("\033[2J\033[H\033[?25l")

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
			selectAndExit(old_state, sentinel_path, options[cursor])

		// Escape or q — cancel
		case n == 1 && (buf[0] == 0x1b || buf[0] == 'q'):
			selectAndExit(old_state, sentinel_path, "")

		// Shortcut key — match "[key]" prefix in option labels
		case n == 1 && buf[0] >= '!' && buf[0] <= '~':
			key := string(buf[0])
			for i, opt := range options {
				if len(opt) >= 3 && opt[0] == '[' && opt[2] == ']' && string(opt[1]) == key {
					cursor = i
					draw()
					time.Sleep(100 * time.Millisecond)
					selectAndExit(old_state, sentinel_path, opt)
				}
			}
		}
	}
}
