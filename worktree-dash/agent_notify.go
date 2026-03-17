package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/elvisnm/wt/internal/sentinel"
)

// runAgentNotify handles the `wt _agent-notify` subcommand.
// Called by Claude Code hooks when the agent enters idle or permission state.
// Writes the event to a sentinel file for the dashboard to pick up.
//
// Usage: wt _agent-notify --event idle_prompt|permission_prompt [--worktree name]
func runAgentNotify(args []string) {
	// Only notify if running inside a wt dashboard tmux session.
	// Check two signals: WT_SOCKET env var (set on the tmux server),
	// or TMUX env var containing a wt- socket path.
	if os.Getenv("WT_SOCKET") == "" && !strings.Contains(os.Getenv("TMUX"), "/wt-") {
		os.Exit(0)
	}

	event := ""
	worktree_name := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--event":
			if i+1 < len(args) {
				event = args[i+1]
				i++
			}
		case "--worktree":
			if i+1 < len(args) {
				worktree_name = args[i+1]
				i++
			}
		}
	}

	if event == "" {
		fmt.Fprintln(os.Stderr, "wt _agent-notify: --event required")
		os.Exit(1)
	}

	// Try to detect worktree name from CWD if not provided
	if worktree_name == "" {
		if cwd, err := os.Getwd(); err == nil {
			worktree_name = filepath.Base(cwd)
			// Strip "feat-" prefix for display
			worktree_name = strings.TrimPrefix(worktree_name, "feat-")
		}
	}

	// Write sentinel with unique name to avoid races between multiple Claude sessions
	name := fmt.Sprintf("%s-%d", sentinel.AgentNotify, os.Getpid())
	content := fmt.Sprintf("%s\n%s\n", event, worktree_name)
	sentinel.Write(name, content)
}
