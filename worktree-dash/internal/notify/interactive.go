package notify

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/term"
)

// run_picker handles an interactive picker session. Blocks until the user
// selects an option or cancels. Writes the result to the sentinel file.
// Returns the number of rows used (for the caller to know the pane height).
func run_picker(title string, options []string, sentinel_path string, width int) {
	if len(options) == 0 {
		return
	}

	old_state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return
	}
	defer term.Restore(int(os.Stdin.Fd()), old_state)

	cursor := 0
	total := len(options)

	draw := func() {
		fmt.Print(ansiClearScreen + ansiHideCursor)
		fmt.Print(RenderPicker(title, options, cursor, width))
	}

	write_result := func(result string) {
		term.Restore(int(os.Stdin.Fd()), old_state)
		fmt.Print(ansiShowCursor)
		if sentinel_path != "" {
			os.WriteFile(sentinel_path, []byte(result+"\n"), 0644)
		}
	}

	draw()

	buf := make([]byte, 3)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			continue
		}

		switch {
		// Arrow up
		case n == 3 && buf[0] == 0x1b && buf[1] == '[' && buf[2] == 'A':
			cursor = (cursor - 1 + total) % total
			draw()

		// Arrow down
		case n == 3 && buf[0] == 0x1b && buf[1] == '[' && buf[2] == 'B':
			cursor = (cursor + 1) % total
			draw()

		// Enter
		case n == 1 && (buf[0] == '\r' || buf[0] == '\n'):
			write_result(options[cursor])
			return

		// Escape or q — cancel
		case n == 1 && (buf[0] == 0x1b || buf[0] == 'q'):
			write_result("")
			return

		// Ctrl+C — cancel
		case n == 1 && buf[0] == 0x03:
			write_result("")
			return

		// Shortcut key — match "[key]" prefix in option labels
		case n == 1 && buf[0] >= '!' && buf[0] <= '~':
			key := string(buf[0])
			for i, opt := range options {
				if len(opt) >= 3 && opt[0] == '[' && opt[2] == ']' && string(opt[1]) == key {
					cursor = i
					draw()
					time.Sleep(100 * time.Millisecond)
					write_result(opt)
					return
				}
			}
		}
	}
}

// run_confirm handles an interactive yes/no confirmation. Blocks until the
// user confirms or cancels. Writes "yes" or "" to the sentinel file.
func run_confirm(title, prompt, sentinel_path string, width int) {
	old_state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return
	}
	defer term.Restore(int(os.Stdin.Fd()), old_state)

	cursor := 0 // 0 = Yes, 1 = No

	draw := func() {
		fmt.Print(ansiClearScreen + ansiHideCursor)
		fmt.Print(RenderConfirm(title, prompt, cursor, width))
	}

	write_result := func(result string) {
		term.Restore(int(os.Stdin.Fd()), old_state)
		fmt.Print(ansiShowCursor)
		if sentinel_path != "" {
			os.WriteFile(sentinel_path, []byte(result+"\n"), 0644)
		}
	}

	draw()

	buf := make([]byte, 3)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			continue
		}

		switch {
		// Arrow left / up
		case n == 3 && buf[0] == 0x1b && buf[1] == '[' && (buf[2] == 'D' || buf[2] == 'A'):
			cursor = 0
			draw()

		// Arrow right / down
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
			return

		// y
		case n == 1 && buf[0] == 'y':
			write_result("yes")
			return

		// n, Escape, q
		case n == 1 && (buf[0] == 'n' || buf[0] == 0x1b || buf[0] == 'q'):
			write_result("")
			return

		// Ctrl+C
		case n == 1 && buf[0] == 0x03:
			write_result("")
			return
		}
	}
}

// run_input handles an interactive text input. Blocks until the user
// submits or cancels. Writes the value or "" to the sentinel file.
func run_input(title, prompt, sentinel_path string, width int) {
	old_state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return
	}
	defer term.Restore(int(os.Stdin.Fd()), old_state)

	value := ""

	draw := func() {
		fmt.Print(ansiClearScreen + ansiHideCursor)
		fmt.Print(RenderInput(title, prompt, value, width))
	}

	write_result := func(result string) {
		term.Restore(int(os.Stdin.Fd()), old_state)
		fmt.Print(ansiShowCursor)
		if sentinel_path != "" {
			os.WriteFile(sentinel_path, []byte(result+"\n"), 0644)
		}
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
			return

		// Escape — cancel
		case n == 1 && buf[0] == 0x1b:
			write_result("")
			return

		// Ctrl+C — cancel
		case n == 1 && buf[0] == 0x03:
			write_result("")
			return

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
