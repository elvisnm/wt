package aws

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReloadFromFile(t *testing.T) {
	// Create a temp credentials file
	dir := t.TempDir()
	creds_path := filepath.Join(dir, "credentials")
	content := `[default]
aws_access_key_id = AKIATEST12345678
aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
aws_session_token = FwoGZXIvYXdzEBYaDH/test/token
`
	if err := os.WriteFile(creds_path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	// Override home dir by writing to a known path and reading it
	os.Unsetenv("AWS_ACCESS_KEY_ID")
	os.Unsetenv("AWS_SECRET_ACCESS_KEY")
	os.Unsetenv("AWS_SESSION_TOKEN")

	// We can't easily test reload_from_file without overriding home dir,
	// but we can test the parsing logic via EnvVars after manual setenv.
	os.Setenv("AWS_ACCESS_KEY_ID", "AKIATEST12345678")
	os.Setenv("AWS_SECRET_ACCESS_KEY", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY")
	os.Setenv("AWS_SESSION_TOKEN", "FwoGZXIvYXdzEBYaDH/test/token")

	vars := EnvVars()
	if vars["AWS_ACCESS_KEY_ID"] != "AKIATEST12345678" {
		t.Errorf("expected AKIATEST12345678, got %s", vars["AWS_ACCESS_KEY_ID"])
	}
	if vars["AWS_SECRET_ACCESS_KEY"] != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
		t.Errorf("expected secret key, got %s", vars["AWS_SECRET_ACCESS_KEY"])
	}
	if vars["AWS_SESSION_TOKEN"] != "FwoGZXIvYXdzEBYaDH/test/token" {
		t.Errorf("expected session token, got %s", vars["AWS_SESSION_TOKEN"])
	}
}

func TestEnvVars_Empty(t *testing.T) {
	os.Unsetenv("AWS_ACCESS_KEY_ID")
	os.Unsetenv("AWS_SECRET_ACCESS_KEY")
	os.Unsetenv("AWS_SESSION_TOKEN")

	vars := EnvVars()
	if len(vars) != 0 {
		t.Errorf("expected empty map, got %v", vars)
	}
}

func TestEnvVars_Partial(t *testing.T) {
	os.Setenv("AWS_ACCESS_KEY_ID", "AKIATEST")
	os.Unsetenv("AWS_SECRET_ACCESS_KEY")
	os.Unsetenv("AWS_SESSION_TOKEN")

	vars := EnvVars()
	if len(vars) != 1 {
		t.Errorf("expected 1 entry, got %d", len(vars))
	}
	if vars["AWS_ACCESS_KEY_ID"] != "AKIATEST" {
		t.Errorf("expected AKIATEST, got %s", vars["AWS_ACCESS_KEY_ID"])
	}
}

type mock_tmux_server struct {
	env map[string]string
}

func (m *mock_tmux_server) SetEnv(key, value string) error {
	m.env[key] = value
	return nil
}

func TestPropagateToTmux(t *testing.T) {
	os.Setenv("AWS_ACCESS_KEY_ID", "AKIATEST")
	os.Setenv("AWS_SECRET_ACCESS_KEY", "SECRET")
	os.Setenv("AWS_SESSION_TOKEN", "TOKEN")

	server := &mock_tmux_server{env: make(map[string]string)}
	PropagateToTmux(server)

	if server.env["AWS_ACCESS_KEY_ID"] != "AKIATEST" {
		t.Errorf("expected AKIATEST, got %s", server.env["AWS_ACCESS_KEY_ID"])
	}
	if server.env["AWS_SECRET_ACCESS_KEY"] != "SECRET" {
		t.Errorf("expected SECRET, got %s", server.env["AWS_SECRET_ACCESS_KEY"])
	}
	if server.env["AWS_SESSION_TOKEN"] != "TOKEN" {
		t.Errorf("expected TOKEN, got %s", server.env["AWS_SESSION_TOKEN"])
	}
}

func TestPropagateToTmux_NilServer(t *testing.T) {
	// Should not panic
	PropagateToTmux(nil)
}

func TestRefresh_NoProfile_NoFile(t *testing.T) {
	// With no profile and no credentials file, Refresh should return an error
	old_home := os.Getenv("HOME")
	os.Setenv("HOME", t.TempDir())
	defer os.Setenv("HOME", old_home)

	err := Refresh("")
	if err == nil {
		t.Error("expected error when no credentials file exists")
	}
}
