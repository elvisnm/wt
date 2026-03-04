package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ErrUnauthorized is returned when the API responds with 401.
var ErrUnauthorized = errors.New("401 unauthorized")

var http_client = &http.Client{Timeout: 10 * time.Second}

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

// Tokens holds access and refresh tokens read from the keychain.
type Tokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    int64 // Unix milliseconds
}

// credentials is the JSON structure stored in macOS Keychain.
type credentials struct {
	ClaudeAiOauth struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresAt    int64  `json:"expiresAt"`
	} `json:"claudeAiOauth"`
}

const (
	keychainService = "Claude Code-credentials"
	tokenURL        = "https://platform.claude.com/v1/oauth/token"
	clientID        = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
)

// read_keychain_raw reads the raw credentials JSON from macOS Keychain.
func read_keychain_raw() ([]byte, error) {
	out, err := exec.Command(
		"security", "find-generic-password",
		"-s", keychainService,
		"-w",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("keychain read failed: %w", err)
	}
	return out, nil
}

// FetchTokens reads the Claude Code OAuth tokens from macOS Keychain.
func FetchTokens() (*Tokens, error) {
	out, err := read_keychain_raw()
	if err != nil {
		return nil, err
	}

	var creds credentials
	if err := json.Unmarshal(out, &creds); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}

	if creds.ClaudeAiOauth.AccessToken == "" {
		return nil, fmt.Errorf("no accessToken in credentials")
	}
	return &Tokens{
		AccessToken:  creds.ClaudeAiOauth.AccessToken,
		RefreshToken: creds.ClaudeAiOauth.RefreshToken,
		ExpiresAt:    creds.ClaudeAiOauth.ExpiresAt,
	}, nil
}

// FetchToken reads just the access token from macOS Keychain (convenience wrapper).
func FetchToken() (string, error) {
	t, err := FetchTokens()
	if err != nil {
		return "", err
	}
	return t.AccessToken, nil
}

// RefreshAccessToken exchanges a refresh token for a new access token.
// Returns updated tokens. The refresh token rotates — caller must persist the new one.
func RefreshAccessToken(refresh_token string) (*Tokens, error) {
	body, _ := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refresh_token,
		"client_id":     clientID,
		"scope":         "user:profile user:inference user:sessions:claude_code user:mcp_servers",
	})

	resp, err := http_client.Post(tokenURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token refresh %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parse refresh response: %w", err)
	}

	// Fall back to original refresh token if server doesn't rotate
	rt := result.RefreshToken
	if rt == "" {
		rt = refresh_token
	}

	return &Tokens{
		AccessToken:  result.AccessToken,
		RefreshToken: rt,
		ExpiresAt:    time.Now().UnixMilli() + result.ExpiresIn*1000,
	}, nil
}

// WriteTokens updates the keychain credentials with new tokens.
// Uses map[string]json.RawMessage at both levels to preserve all unknown fields.
func WriteTokens(tokens *Tokens) error {
	out, err := read_keychain_raw()
	if err != nil {
		return err
	}

	// Preserve all top-level fields (mcpOAuth, etc.)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(out, &raw); err != nil {
		return fmt.Errorf("parse existing credentials: %w", err)
	}

	// Preserve all fields inside claudeAiOauth (subscriptionType, rateLimitTier, scopes, etc.)
	var inner map[string]json.RawMessage
	if oauth_raw, ok := raw["claudeAiOauth"]; ok {
		if err := json.Unmarshal(oauth_raw, &inner); err != nil {
			return fmt.Errorf("parse existing oauth entry: %w", err)
		}
	} else {
		inner = make(map[string]json.RawMessage)
	}

	// Patch only the token fields
	inner["accessToken"], _ = json.Marshal(tokens.AccessToken)
	inner["refreshToken"], _ = json.Marshal(tokens.RefreshToken)
	inner["expiresAt"], _ = json.Marshal(tokens.ExpiresAt)

	updated, err := json.Marshal(inner)
	if err != nil {
		return fmt.Errorf("marshal oauth entry: %w", err)
	}
	raw["claudeAiOauth"] = updated

	full, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	// Update keychain entry (-U updates existing, -a matches the account)
	account := os.Getenv("USER")
	cmd := exec.Command(
		"security", "add-generic-password",
		"-U",
		"-a", account,
		"-s", keychainService,
		"-w", string(full),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keychain write failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// FetchUsage calls the Anthropic usage API and returns current utilization.
func FetchUsage(token string) (*Usage, error) {
	req, err := http.NewRequest("GET", "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")

	resp, err := http_client.Do(req)
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
