package main

import (
	"io"
	"os"
	"syscall"
	"unsafe"
)

// disableEcho puts the terminal into a mode where input is not echoed back.
// This prevents keypresses and mouse clicks from showing raw escape sequences.
func disableEcho() {
	fd := int(os.Stdin.Fd())
	var termios syscall.Termios
	if _, _, err := syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd),
		uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&termios)),
		0, 0, 0); err != 0 {
		return
	}
	termios.Lflag &^= syscall.ECHO | syscall.ICANON
	syscall.Syscall6(syscall.SYS_IOCTL, uintptr(fd),
		uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&termios)),
		0, 0, 0)
	// Drain stdin so bytes don't queue up
	go io.Copy(io.Discard, os.Stdin)
}
