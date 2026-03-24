package aws

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// TmuxEnvSetter is satisfied by terminal.TmuxServer.
type TmuxEnvSetter interface {
	SetEnv(key, value string) error
}

// env_keys are the AWS env vars we manage.
var env_keys = []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN"}

// Refresh loads AWS credentials into the current process env.
// If profile is non-empty, exports from the SSO session via
// `aws configure export-credentials`. Otherwise reads ~/.aws/credentials.
// Returns an error if credentials could not be loaded.
func Refresh(profile string) error {
	if profile != "" {
		return export_sso(profile)
	}
	return reload_from_file()
}

// PropagateToTmux sets AWS env vars on the tmux server so new windows inherit them.
func PropagateToTmux(server TmuxEnvSetter) {
	if server == nil {
		return
	}
	for _, key := range env_keys {
		if val := os.Getenv(key); val != "" {
			server.SetEnv(key, val)
		}
	}
}

// CheckSession runs `aws sts get-caller-identity` to verify the SSO session.
// Returns true if the session is valid, false if expired or error.
func CheckSession(profile string) bool {
	cmd := exec.Command("aws", "sts", "get-caller-identity", "--profile", profile, "--output", "json")
	cmd.Env = os.Environ()
	return cmd.Run() == nil
}

// EnvVars returns the current AWS env vars as a map.
// Only includes keys that are set and non-empty.
func EnvVars() map[string]string {
	result := make(map[string]string, len(env_keys))
	for _, key := range env_keys {
		if val := os.Getenv(key); val != "" {
			result[key] = val
		}
	}
	return result
}

// export_sso runs `aws configure export-credentials` to extract temporary
// credentials from the SSO session, writes them to ~/.aws/credentials,
// and sets them as env vars so child processes inherit them.
func export_sso(profile string) error {
	cmd := exec.Command("aws", "configure", "export-credentials", "--profile", profile, "--format", "env-no-export")
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("aws configure export-credentials failed: %w", err)
	}

	// Parse KEY=VALUE lines
	creds := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		creds[parts[0]] = parts[1]
	}

	access_key := creds["AWS_ACCESS_KEY_ID"]
	secret_key := creds["AWS_SECRET_ACCESS_KEY"]
	session_token := creds["AWS_SESSION_TOKEN"]

	if access_key == "" || secret_key == "" {
		return fmt.Errorf("export-credentials returned incomplete credentials")
	}

	// Unset AWS_PROFILE so the SDK uses the explicit keys instead of resolving
	// from the SSO profile cache (which may hold stale credentials).
	os.Unsetenv("AWS_PROFILE")
	os.Setenv("AWS_ACCESS_KEY_ID", access_key)
	os.Setenv("AWS_SECRET_ACCESS_KEY", secret_key)
	os.Setenv("AWS_SESSION_TOKEN", session_token)

	// Write to ~/.aws/credentials so the SDK credential chain picks them up
	home, err := os.UserHomeDir()
	if err != nil {
		return nil // env vars are set, credentials file is best-effort
	}
	creds_content := fmt.Sprintf("[default]\naws_access_key_id = %s\naws_secret_access_key = %s\naws_session_token = %s\n",
		access_key, secret_key, session_token)
	creds_path := filepath.Join(home, ".aws", "credentials")
	if err := os.WriteFile(creds_path, []byte(creds_content), 0600); err != nil {
		return nil // env vars are set, credentials file is best-effort
	}

	return nil
}

// reload_from_file reads ~/.aws/credentials and sets env vars.
func reload_from_file() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("UserHomeDir: %w", err)
	}
	creds_path := filepath.Join(home, ".aws", "credentials")
	data, err := os.ReadFile(creds_path)
	if err != nil {
		return fmt.Errorf("read %s: %w", creds_path, err)
	}

	keys_set := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") || line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "aws_access_key_id":
			os.Setenv("AWS_ACCESS_KEY_ID", val)
			keys_set++
		case "aws_secret_access_key":
			os.Setenv("AWS_SECRET_ACCESS_KEY", val)
			keys_set++
		case "aws_session_token":
			os.Setenv("AWS_SESSION_TOKEN", val)
			keys_set++
		}
	}
	if keys_set == 0 {
		return fmt.Errorf("no credentials found in %s", creds_path)
	}
	return nil
}
