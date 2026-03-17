package agent

import (
	"fmt"
	"testing"
	"time"
)

func TestDetectIdlePrompt(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    idle_result
	}{
		{
			name: "claude finished - empty prompt with esc hint",
			content: `some output here
─────────────────────────────────────────────────
❯
─────────────────────────────────────────────────
  PR #10396 · esc to interrupt`,
			want: idle_finished,
		},
		{
			name: "claude finished - welcome screen",
			content: `  ╰────────────────────────────╯
                    Version dev - @elvisnm
                  Press ? for all keybindings`,
			want: idle_finished,
		},
		{
			name: "claude waiting - tool approval",
			content: `Do you want to proceed?
❯ 1. Yes
  2. No`,
			want: idle_waiting,
		},
		{
			name: "claude waiting - any selector text",
			content: `Which option?
❯ Some action
  Another action`,
			want: idle_waiting,
		},
		{
			name: "claude working - spinner",
			content: `⠋ Thinking...

  some code being written`,
			want: not_idle,
		},
		{
			name: "empty content",
			content: "",
			want: not_idle,
		},
		{
			name: "bash prompt",
			content: `$ ls -la
total 42
$`,
			want: not_idle,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detect_idle_prompt(tt.content)
			if got != tt.want {
				t.Errorf("detect_idle_prompt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMonitorWatchAndIdle(t *testing.T) {
	call_count := 0
	// Simulate: first 3 calls return changing content (triggers Working),
	// then stable idle content (triggers Idle after threshold)
	capture := func(pane_id string) (string, error) {
		call_count++
		if call_count <= 3 {
			return fmt.Sprintf("working step %d", call_count), nil
		}
		return "────────────────────────────\n❯\n────────────────────────────\n  esc to interrupt", nil
	}

	mon := NewMonitor(capture)
	mon.stable_threshold = 50 * time.Millisecond
	mon.poll_interval = 20 * time.Millisecond

	mon.Watch("claude:test", "test", "%5")

	// Should get: working (from changing content) then idle (from stable prompt)
	deadline := time.After(2 * time.Second)
	for {
		select {
		case evt := <-mon.Events():
			if evt.Idle {
				t.Logf("Got idle event: label=%s finished=%v", evt.Label, evt.Finished)
				goto DONE
			}
			// "working" events are expected before idle
		case <-deadline:
			t.Fatal("Timeout waiting for idle event")
		}
	}
DONE:
	mon.UnwatchAll()
}

func TestMonitorResume(t *testing.T) {
	phase := 0 // 0=working, 1=idle, 2=working again
	capture := func(pane_id string) (string, error) {
		switch phase {
		case 0:
			return fmt.Sprintf("working %d", time.Now().UnixNano()), nil
		case 1:
			return "────────────────────────────\n❯\n────────────────────────────", nil
		default:
			return fmt.Sprintf("new work %d", time.Now().UnixNano()), nil
		}
	}

	mon := NewMonitor(capture)
	mon.stable_threshold = 30 * time.Millisecond
	mon.poll_interval = 15 * time.Millisecond

	mon.Watch("claude:test", "test", "%5")

	// Phase 0: working — wait for working event
	deadline := time.After(2 * time.Second)
	for {
		select {
		case evt := <-mon.Events():
			if !evt.Idle {
				goto GOT_WORKING
			}
		case <-deadline:
			t.Fatal("Timeout waiting for working")
		}
	}
GOT_WORKING:
	// Phase 1: idle
	phase = 1
	for {
		select {
		case evt := <-mon.Events():
			if evt.Idle {
				goto GOT_IDLE
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Timeout waiting for idle")
		}
	}
GOT_IDLE:
	// Phase 2: working again — should get resume event
	phase = 2
	select {
	case evt := <-mon.Events():
		if evt.Idle {
			t.Error("Expected resume event, got idle")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for resume")
	}

	mon.UnwatchAll()
}

func TestMonitorSkipsInitialIdle(t *testing.T) {
	// Session starts with idle content — should NOT trigger notification
	// because it was never working first
	capture := func(pane_id string) (string, error) {
		return "Press ? for all keybindings", nil
	}

	mon := NewMonitor(capture)
	mon.stable_threshold = 30 * time.Millisecond
	mon.poll_interval = 15 * time.Millisecond

	mon.Watch("claude:test", "test", "%5")

	select {
	case evt := <-mon.Events():
		t.Errorf("Should not get event for initial idle, got: idle=%v", evt.Idle)
	case <-time.After(200 * time.Millisecond):
		// Good — no event fired
	}

	mon.UnwatchAll()
}
