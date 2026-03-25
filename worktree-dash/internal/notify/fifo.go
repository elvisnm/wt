package notify

import (
	"encoding/json"
	"os"
	"syscall"
)

// FifoPath returns the FIFO path for a given tmux server PID.
// Placed in os.TempDir() alongside sentinel files.
func FifoPath(pid int) string {
	return os.TempDir() + "/wt-notify-" + itoa(pid)
}

// SendCommand writes a JSON command to the FIFO.
// Non-blocking: opens with O_WRONLY|O_NONBLOCK so it returns immediately
// if no reader is connected (renderer not running).
func SendCommand(fifo_path string, cmd *Command) error {
	data, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	f, err := os.OpenFile(fifo_path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(data)
	return err
}

// SendClear sends a clear command to the notify renderer.
func SendClear(fifo_path string) error {
	return SendCommand(fifo_path, &Command{Cmd: CmdClear})
}

// SendNotify sends a notification with title and message.
func SendNotify(fifo_path, title, message string) error {
	return SendCommand(fifo_path, &Command{
		Cmd:     CmdNotify,
		Title:   title,
		Message: message,
	})
}

// SendPicker sends a picker command with options.
func SendPicker(fifo_path, title string, options []string, sentinel string) error {
	return SendCommand(fifo_path, &Command{
		Cmd:      CmdPicker,
		Title:    title,
		Options:  options,
		Sentinel: sentinel,
	})
}

// SendConfirm sends a confirmation dialog command.
func SendConfirm(fifo_path, title, prompt, sentinel string) error {
	return SendCommand(fifo_path, &Command{
		Cmd:      CmdConfirm,
		Title:    title,
		Prompt:   prompt,
		Sentinel: sentinel,
	})
}

// SendInput sends a text input dialog command.
func SendInput(fifo_path, title, prompt, sentinel string) error {
	return SendCommand(fifo_path, &Command{
		Cmd:      CmdInput,
		Title:    title,
		Prompt:   prompt,
		Sentinel: sentinel,
	})
}

// itoa converts an int to string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
