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
	raw := `{"claudeAiOauth": {"accessToken": "test-token-123", "refreshToken": "test-refresh-456", "expiresAt": 1772663177232}}`

	var c credentials
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.ClaudeAiOauth.AccessToken != "test-token-123" {
		t.Errorf("accessToken: got %q, want %q", c.ClaudeAiOauth.AccessToken, "test-token-123")
	}
	if c.ClaudeAiOauth.RefreshToken != "test-refresh-456" {
		t.Errorf("refreshToken: got %q, want %q", c.ClaudeAiOauth.RefreshToken, "test-refresh-456")
	}
	if c.ClaudeAiOauth.ExpiresAt != 1772663177232 {
		t.Errorf("expiresAt: got %d, want 1772663177232", c.ClaudeAiOauth.ExpiresAt)
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

func TestWriteTokensPreservesUnknownFields(t *testing.T) {
	raw := `{"claudeAiOauth":{"accessToken":"old","refreshToken":"old-ref","expiresAt":100,"subscriptionType":"team","rateLimitTier":"default"},"mcpOAuth":{"key":"val"}}`

	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &top); err != nil {
		t.Fatalf("unmarshal top: %v", err)
	}

	// Simulate what WriteTokens does: patch inner fields via map
	var inner map[string]json.RawMessage
	if err := json.Unmarshal(top["claudeAiOauth"], &inner); err != nil {
		t.Fatalf("unmarshal inner: %v", err)
	}
	inner["accessToken"], _ = json.Marshal("new-token")
	inner["refreshToken"], _ = json.Marshal("new-ref")
	inner["expiresAt"], _ = json.Marshal(int64(999))

	updated, _ := json.Marshal(inner)
	top["claudeAiOauth"] = updated
	full, _ := json.Marshal(top)

	// Verify unknown fields survived round-trip
	var check map[string]json.RawMessage
	json.Unmarshal(full, &check)

	// mcpOAuth should be preserved
	if _, ok := check["mcpOAuth"]; !ok {
		t.Error("mcpOAuth field was lost")
	}

	// subscriptionType inside claudeAiOauth should be preserved
	var inner_check map[string]json.RawMessage
	json.Unmarshal(check["claudeAiOauth"], &inner_check)
	if _, ok := inner_check["subscriptionType"]; !ok {
		t.Error("subscriptionType field was lost inside claudeAiOauth")
	}
	if _, ok := inner_check["rateLimitTier"]; !ok {
		t.Error("rateLimitTier field was lost inside claudeAiOauth")
	}

	// Patched fields should have new values
	var new_token string
	json.Unmarshal(inner_check["accessToken"], &new_token)
	if new_token != "new-token" {
		t.Errorf("accessToken: got %q, want %q", new_token, "new-token")
	}
}
