package agent

import (
	"testing"
	"time"
)

func TestDetectIdlePrompt(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name: "claude idle - real format",
			content: `some output here
─────────────────────────────────────────────────
❯
─────────────────────────────────────────────────
  PR #10396 · esc to interrupt`,
			want: true,
		},
		{
			name: "claude idle - minimal",
			content: `────────────────────────────────
❯
────────────────────────────────`,
			want: true,
		},
		{
			name: "claude idle - with esc hint only",
			content: `❯
  esc to interrupt`,
			want: true,
		},
		{
			name: "claude working - spinner visible",
			content: `⠋ Thinking...

  some code being written`,
			want: false,
		},
		{
			name: "empty content",
			content: "",
			want: false,
		},
		{
			name: "bash prompt - no separator",
			content: `$ ls -la
total 42
drwxr-xr-x  5 user staff 160 Mar 16 12:00 .
$`,
			want: false,
		},
		{
			name: "separator without prompt",
			content: `some text
─────────────────────────────────────────
more text
─────────────────────────────────────────`,
			want: false,
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
	// Simulate: first 2 calls return changing content, then stable idle content
	capture := func(pane_id string) (string, error) {
		call_count++
		if call_count <= 2 {
			return "working " + string(rune('0'+call_count)), nil
		}
		return "────────────────────────────\n❯\n────────────────────────────\n  esc to interrupt", nil
	}

	mon := NewMonitor(capture)
	mon.stable_threshold = 50 * time.Millisecond
	mon.poll_interval = 20 * time.Millisecond

	mon.Watch("claude:test", "test", "%5")

	// Wait for detection
	select {
	case evt := <-mon.Events():
		if evt.Idle {
			t.Logf("Got idle event: label=%s alias=%s", evt.Label, evt.Alias)
		} else {
			// First event might be "working" — wait for idle
			select {
			case evt2 := <-mon.Events():
				if !evt2.Idle {
					t.Error("Expected idle event, got working")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Timeout waiting for idle event")
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for any event")
	}

	mon.UnwatchAll()
}

func TestMonitorResume(t *testing.T) {
	content := "────────────────────────────\n❯\n────────────────────────────\n  esc to interrupt"
	capture := func(pane_id string) (string, error) {
		return content, nil
	}

	mon := NewMonitor(capture)
	mon.stable_threshold = 30 * time.Millisecond
	mon.poll_interval = 15 * time.Millisecond

	mon.Watch("claude:test", "test", "%5")

	// Wait for idle (may get a working event first)
	deadline := time.After(2 * time.Second)
	for {
		select {
		case evt := <-mon.Events():
			if evt.Idle {
				goto GOT_IDLE
			}
		case <-deadline:
			t.Fatal("Timeout waiting for idle")
		}
	}
GOT_IDLE:

	// Change content — should get resume event
	content = "working on something new"
	select {
	case evt := <-mon.Events():
		if evt.Idle {
			t.Error("Expected resume (working) event")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for resume")
	}

	mon.UnwatchAll()
}
