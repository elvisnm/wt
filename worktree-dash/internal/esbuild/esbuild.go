package esbuild

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const pid_filename = "esbuild.pid"
const log_filename = "esbuild.log"

// Start launches the esbuild watcher as a detached background process.
// build_script is the absolute path to the build script (e.g., scripts/deployment_scripts/build.js).
// wt_path is the worktree directory (used as cwd).
// state_dir is where PID and log files are stored (e.g., .pm2 dir).
// extra_env holds additional env vars (WORKTREE_PORT_OFFSET, SKULABS_ENV, etc.).
func Start(build_script string, wt_path string, state_dir string, extra_env []string) error {
	if IsRunning(state_dir) {
		return nil // already running
	}

	os.MkdirAll(state_dir, 0755)

	log_path := filepath.Join(state_dir, log_filename)
	log_file, err := os.OpenFile(log_path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	cmd := exec.Command("node", build_script, "develop", "--watch")
	cmd.Dir = wt_path
	cmd.Env = append(os.Environ(), extra_env...)
	cmd.Stdout = log_file
	cmd.Stderr = log_file
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // detach from parent process group
	}

	if err := cmd.Start(); err != nil {
		log_file.Close()
		return fmt.Errorf("start esbuild: %w", err)
	}

	log_file.Close()

	// Write PID file
	pid_path := filepath.Join(state_dir, pid_filename)
	os.WriteFile(pid_path, []byte(strconv.Itoa(cmd.Process.Pid)), 0644)

	// Don't wait — let it run detached
	go cmd.Wait()

	return nil
}

// Stop kills the esbuild watcher process if running.
func Stop(state_dir string) error {
	pid := read_pid(state_dir)
	if pid == 0 {
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		cleanup_pid(state_dir)
		return nil
	}

	// Kill the process group to catch any children
	syscall.Kill(-pid, syscall.SIGTERM)
	proc.Wait()
	cleanup_pid(state_dir)
	return nil
}

// IsRunning checks if the esbuild watcher is alive via the PID file.
func IsRunning(state_dir string) bool {
	pid := read_pid(state_dir)
	if pid == 0 {
		return false
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		cleanup_pid(state_dir)
		return false
	}

	// Signal 0 checks if process exists without killing it
	err = proc.Signal(syscall.Signal(0))
	if err != nil {
		cleanup_pid(state_dir)
		return false
	}
	return true
}

// LogPath returns the path to the esbuild log file.
func LogPath(state_dir string) string {
	return filepath.Join(state_dir, log_filename)
}

func read_pid(state_dir string) int {
	pid_path := filepath.Join(state_dir, pid_filename)
	data, err := os.ReadFile(pid_path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	return pid
}

func cleanup_pid(state_dir string) {
	os.Remove(filepath.Join(state_dir, pid_filename))
}
