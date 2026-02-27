package config

import (
	"path/filepath"
	"runtime"
	"testing"
)

// testdataDir returns the testdata directory next to this test file.
func testdataDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine test file path")
	}
	return filepath.Join(filepath.Dir(file), "testdata")
}

func TestLoad(t *testing.T) {
	dir := testdataDir(t)
	config_path := filepath.Join(dir, "workflow.config.js")

	cfg, err := LoadFromPath(config_path, dir)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Name != "myapp" {
		t.Errorf("expected name 'myapp', got %q", cfg.Name)
	}

	if cfg.RepoRoot != dir {
		t.Errorf("expected repoRoot %q, got %q", dir, cfg.RepoRoot)
	}

	// Container name
	if cn := cfg.ContainerName("my-feat"); cn != "myapp-my-feat" {
		t.Errorf("ContainerName: expected 'myapp-my-feat', got %q", cn)
	}

	// Container prefix
	if cp := cfg.ContainerPrefix(); cp != "myapp-" {
		t.Errorf("ContainerPrefix: expected 'myapp-', got %q", cp)
	}

	// Container filter
	if cf := cfg.ContainerFilter(); cf != "name=myapp-" {
		t.Errorf("ContainerFilter: expected 'name=myapp-', got %q", cf)
	}

	// Domain
	if d := cfg.DomainFor("my-feat"); d != "my-feat.localhost" {
		t.Errorf("DomainFor: expected 'my-feat.localhost', got %q", d)
	}

	// Database name
	if dn := cfg.DbName("my-feat"); dn != "db_my_feat" {
		t.Errorf("DbName: expected 'db_my_feat', got %q", dn)
	}

	// Services
	if port, ok := cfg.Services.Ports["api"]; !ok || port != 3001 {
		t.Errorf("expected api port 3001, got %d", port)
	}

	if cfg.Services.Primary != "web" {
		t.Errorf("expected primary 'web', got %q", cfg.Services.Primary)
	}

	// Env vars
	if v := cfg.EnvVar("dbConnection"); v != "MYAPP_MONGO_URL" {
		t.Errorf("EnvVar(dbConnection): expected 'MYAPP_MONGO_URL', got %q", v)
	}

	if v := cfg.WorktreeVar("alias"); v != "WORKTREE_ALIAS" {
		t.Errorf("WorktreeVar(alias): expected 'WORKTREE_ALIAS', got %q", v)
	}

	// Features
	if !cfg.FeatureEnabled("hostBuild") {
		t.Error("expected hostBuild enabled")
	}
	if !cfg.FeatureEnabled("admin") {
		t.Error("expected admin enabled")
	}

	// Proxy
	if cfg.Docker.Proxy.Type != "traefik" {
		t.Errorf("expected proxy type 'traefik', got %q", cfg.Docker.Proxy.Type)
	}

	// Database
	if cfg.Database == nil {
		t.Fatal("expected database config")
	}
	if cfg.Database.Type != "mongodb" {
		t.Errorf("expected database type 'mongodb', got %q", cfg.Database.Type)
	}
	if cfg.Database.ContainerHost != "mongo" {
		t.Errorf("expected containerHost 'mongo', got %q", cfg.Database.ContainerHost)
	}

	// Redis
	if cfg.Redis == nil {
		t.Fatal("expected redis config")
	}
	if cfg.Redis.ContainerHost != "redis" {
		t.Errorf("expected redis host 'redis', got %q", cfg.Redis.ContainerHost)
	}

	// Resolved paths
	if cfg.WorktreesDirAbs == "" {
		t.Error("expected WorktreesDirAbs to be set")
	}

	// Minimal services
	minimal := cfg.ServicesForMode("minimal")
	if minimal == nil || len(minimal) != 2 {
		t.Errorf("expected 2 minimal services, got %v", minimal)
	}

	// Full services = nil (all)
	full := cfg.ServicesForMode("full")
	if full != nil {
		t.Errorf("expected nil (all) for full mode, got %v", full)
	}

	// ComputePorts
	ports := cfg.ComputePorts(500)
	if ports["api"] != 3501 {
		t.Errorf("expected api port with offset 500 = 3501, got %d", ports["api"])
	}
}
