package notify

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

// renderer holds the state for the notify renderer process.
type renderer struct {
	fifo_path string
	socket    string // tmux socket name (e.g. "wt-12345")
	pane_id   string // tmux pane ID from TMUX_PANE env (e.g. "%3")
	width     int
	last_cmd  *Command
}

// Run is the main entry point for the notify renderer process.
// It creates a FIFO at fifo_path, listens for JSON commands, and renders
// content directly to the terminal. This process is long-lived — it runs
// for the entire duration of the dashboard session.
//
// The renderer owns its pane's height: it draws content first, then resizes
// the pane via tmux. This eliminates flicker because content is already in
// the terminal buffer when new rows become visible.
func Run(fifo_path, socket string) error {
	if err := create_fifo(fifo_path); err != nil {
		return fmt.Errorf("failed to create FIFO %s: %w", fifo_path, err)
	}
	defer os.Remove(fifo_path)

	r := &renderer{
		fifo_path: fifo_path,
		socket:    socket,
		pane_id:   os.Getenv("TMUX_PANE"),
		width:     get_terminal_width(),
	}

	// Handle SIGWINCH for terminal resize
	sig_ch := make(chan os.Signal, 1)
	signal.Notify(sig_ch, syscall.SIGWINCH)
	go func() {
		for range sig_ch {
			r.width = get_terminal_width()
			if r.last_cmd != nil {
				render_command(r.last_cmd, r.width)
			}
		}
	}()

	// Render initial clear state
	fmt.Print(ansiClearScreen + ansiHideCursor)
	fmt.Print(RenderClear(r.width))
	r.last_cmd = &Command{Cmd: CmdClear}

	// Main loop: open FIFO, read commands, process them.
	// When the writer closes the pipe we get EOF — re-open and listen again.
	for {
		if err := r.read_fifo_loop(); err != nil {
			return err
		}
	}
}

// read_fifo_loop opens the FIFO, reads JSON lines, and dispatches commands.
// Returns nil on EOF (writer closed) so the caller can re-open.
func (r *renderer) read_fifo_loop() error {
	f, err := os.OpenFile(r.fifo_path, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to open FIFO: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		cmd, err := ParseCommand(line)
		if err != nil {
			continue
		}

		r.width = get_terminal_width()
		r.dispatch(cmd)
	}

	return scanner.Err()
}

// dispatch processes a single command: draw content first, then resize pane.
func (r *renderer) dispatch(cmd *Command) {
	switch cmd.Cmd {
	case CmdClear:
		fmt.Print(ansiClearScreen + ansiHideCursor)
		fmt.Print(RenderClear(r.width))
		r.resize_pane(2)
		r.last_cmd = cmd

	case CmdNotify:
		fmt.Print(ansiClearScreen + ansiHideCursor)
		fmt.Print(RenderNotify(cmd.Title, cmd.Message, r.width))
		r.resize_pane(3)
		r.last_cmd = cmd

	case CmdPicker:
		rows := len(cmd.Options) + 2 // options + top/bottom border
		fmt.Print(ansiClearScreen + ansiHideCursor)
		fmt.Print(RenderPicker(cmd.Title, cmd.Options, 0, r.width))
		r.resize_pane(rows)
		// Interactive — blocks until user selects or cancels
		run_picker(cmd.Title, cmd.Options, cmd.Sentinel, r.width)
		// After interactive session ends, show clear state
		fmt.Print(ansiClearScreen + ansiHideCursor)
		fmt.Print(RenderClear(r.width))
		r.resize_pane(2)
		r.last_cmd = &Command{Cmd: CmdClear}

	case CmdConfirm:
		fmt.Print(ansiClearScreen + ansiHideCursor)
		fmt.Print(RenderConfirm(cmd.Title, cmd.Prompt, 0, r.width))
		r.resize_pane(4)
		run_confirm(cmd.Title, cmd.Prompt, cmd.Sentinel, r.width)
		fmt.Print(ansiClearScreen + ansiHideCursor)
		fmt.Print(RenderClear(r.width))
		r.resize_pane(2)
		r.last_cmd = &Command{Cmd: CmdClear}

	case CmdInput:
		fmt.Print(ansiClearScreen + ansiHideCursor)
		fmt.Print(RenderInput(cmd.Title, cmd.Prompt, "", r.width))
		r.resize_pane(3)
		run_input(cmd.Title, cmd.Prompt, cmd.Sentinel, r.width)
		fmt.Print(ansiClearScreen + ansiHideCursor)
		fmt.Print(RenderClear(r.width))
		r.resize_pane(2)
		r.last_cmd = &Command{Cmd: CmdClear}
	}
}

// resize_pane resizes this renderer's tmux pane to the given row count.
// Content is already drawn in the terminal buffer, so newly revealed rows
// show content immediately — no flicker.
func (r *renderer) resize_pane(rows int) {
	if r.socket == "" || r.pane_id == "" {
		return
	}
	exec.Command("tmux", "-L", r.socket,
		"resize-pane", "-t", r.pane_id, "-y", fmt.Sprintf("%d", rows),
	).Run()
}

// render_command re-renders the last command (used on terminal resize).
func render_command(cmd *Command, width int) {
	fmt.Print(ansiClearScreen + ansiHideCursor)
	switch cmd.Cmd {
	case CmdClear:
		fmt.Print(RenderClear(width))
	case CmdNotify:
		fmt.Print(RenderNotify(cmd.Title, cmd.Message, width))
	}
}

// create_fifo creates a named pipe at the given path.
// If the path already exists and is a FIFO, it's reused.
// If it exists but is not a FIFO, it's removed and recreated.
func create_fifo(path string) error {
	info, err := os.Stat(path)
	if err == nil {
		if info.Mode()&os.ModeNamedPipe != 0 {
			return nil
		}
		os.Remove(path)
	}
	return syscall.Mkfifo(path, 0600)
}

// get_terminal_width returns the current terminal width, or 80 as fallback.
func get_terminal_width() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w < 10 {
		return 80
	}
	return w
}
