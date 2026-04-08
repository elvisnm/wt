package app

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var (
	debug_enabled bool
	debug_file    *os.File
)

// debug_init opens the debug log file at $TMPDIR/wt-debug.log.
// Returns immediately if WT_DEBUG != "1". Truncates the file each session.
func debug_init() {
	if os.Getenv("WT_DEBUG") != "1" {
		return
	}
	debug_enabled = true
	p := filepath.Join(os.TempDir(), "wt-debug.log")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		debug_enabled = false
		return
	}
	debug_file = f
	debug_log("[init] === wt debug session started (pid=%d) ===", os.Getpid())
}

// debug_log writes a timestamped line to the debug log file.
// No-op when debug is not enabled (fast bool check, no mutex overhead).
func debug_log(format string, args ...any) {
	if !debug_enabled {
		return
	}
	ts := time.Now().Format("15:04:05.000")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(debug_file, "[%s] %s\n", ts, msg)
	debug_file.Sync()
}
