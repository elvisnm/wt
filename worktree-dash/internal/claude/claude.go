package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// ErrUnauthorized is returned when the API responds with 401.
var ErrUnauthorized = errors.New("401 unauthorized")

var usage_client = &http.Client{Timeout: 10 * time.Second}

// UsagePeriod represents utilization for a single time window.
type UsagePeriod struct {
	Utilization float64   `json:"utilization"`
	ResetsAt    time.Time `json:"resets_at"`
}

// Usage holds the API response for Claude usage limits.
type Usage struct {
	FiveHour UsagePeriod `json:"five_hour"`
	SevenDay UsagePeriod `json:"seven_day"`
}

// credentials is the JSON structure stored in macOS Keychain.
type credentials struct {
	ClaudeAiOauth struct {
		AccessToken string `json:"accessToken"`
	} `json:"claudeAiOauth"`
}

// FetchToken reads the Claude Code OAuth token from macOS Keychain.
func FetchToken() (string, error) {
	out, err := exec.Command(
		"security", "find-generic-password",
		"-s", "Claude Code-credentials",
		"-w",
	).Output()
	if err != nil {
		return "", fmt.Errorf("keychain read failed: %w", err)
	}

	var creds credentials
	if err := json.Unmarshal(out, &creds); err != nil {
		return "", fmt.Errorf("parse credentials: %w", err)
	}

	token := creds.ClaudeAiOauth.AccessToken
	if token == "" {
		return "", fmt.Errorf("no accessToken in credentials")
	}
	return token, nil
}

// FetchUsage calls the Anthropic usage API and returns current utilization.
func FetchUsage(token string) (*Usage, error) {
	req, err := http.NewRequest("GET", "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")

	resp, err := usage_client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("usage request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("usage API %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var usage Usage
	if err := json.NewDecoder(resp.Body).Decode(&usage); err != nil {
		return nil, fmt.Errorf("parse usage: %w", err)
	}
	return &usage, nil
}
