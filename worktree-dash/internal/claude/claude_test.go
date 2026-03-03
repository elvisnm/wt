package claude

import (
	"encoding/json"
	"testing"
	"time"
)

func TestUsageUnmarshal(t *testing.T) {
	raw := `{
		"five_hour": {"utilization": 6.0, "resets_at": "2026-03-03T12:00:00Z"},
		"seven_day": {"utilization": 35.0, "resets_at": "2026-03-07T00:00:00Z"}
	}`

	var u Usage
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if u.FiveHour.Utilization != 6.0 {
		t.Errorf("FiveHour.Utilization: got %f, want 6.0", u.FiveHour.Utilization)
	}
	if u.SevenDay.Utilization != 35.0 {
		t.Errorf("SevenDay.Utilization: got %f, want 35.0", u.SevenDay.Utilization)
	}
	want_5h := time.Date(2026, 3, 3, 12, 0, 0, 0, time.UTC)
	if !u.FiveHour.ResetsAt.Equal(want_5h) {
		t.Errorf("FiveHour.ResetsAt: got %v, want %v", u.FiveHour.ResetsAt, want_5h)
	}
}

func TestCredentialsUnmarshal(t *testing.T) {
	raw := `{"claudeAiOauth": {"accessToken": "test-token-123"}}`

	var c credentials
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.ClaudeAiOauth.AccessToken != "test-token-123" {
		t.Errorf("accessToken: got %q, want %q", c.ClaudeAiOauth.AccessToken, "test-token-123")
	}
}

func TestCredentialsEmptyToken(t *testing.T) {
	raw := `{"claudeAiOauth": {}}`

	var c credentials
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.ClaudeAiOauth.AccessToken != "" {
		t.Errorf("expected empty token, got %q", c.ClaudeAiOauth.AccessToken)
	}
}
