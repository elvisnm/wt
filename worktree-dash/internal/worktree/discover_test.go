package worktree

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/elvisnm/wt/internal/config"
)

// ── Helpers ─────────────────────────────────────────────────────────────

// tmp_dir creates a temp directory that is cleaned up after the test.
func tmp_dir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

// write_file creates a file with the given content inside a directory.
func write_file(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write_file(%s): %v", path, err)
	}
}

// mkdir creates a subdirectory inside a parent directory.
func mkdir(t *testing.T, parent, name string) string {
	t.Helper()
	path := filepath.Join(parent, name)
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir(%s): %v", path, err)
	}
	return path
}

// test_config returns a minimal Config suitable for discovery tests.
func test_config() *config.Config {
	return &config.Config{
		Name: "myapp",
		Env: config.EnvConfig{
			Prefix:   "MYAPP",
			Filename: ".env.worktree",
			Vars: map[string]string{
				"dbConnection": "MYAPP_MONGO_URL",
				"lanDomain":    "MYAPP_LAN_DOMAIN",
			},
			WorktreeVars: map[string]string{
				"alias":          "WORKTREE_ALIAS",
				"hostBuild":      "WORKTREE_HOST_BUILD",
				"services":       "WORKTREE_SERVICES",
				"portOffset":     "WORKTREE_PORT_OFFSET",
				"hostPortOffset": "WORKTREE_HOST_PORT_OFFSET",
				"portBase":       "WORKTREE_PORT_BASE",
			},
		},
		Services: config.ServicesConfig{
			Ports:       map[string]int{"web": 3000, "api": 3001},
			Primary:     "web",
			DefaultMode: "default",
		},
		Docker: config.DockerConfig{
			ComposeStrategy: "generate",
			Proxy: config.ProxyConfig{
				DomainTemplate: "{alias}.localhost",
			},
		},
		Database: &config.DatabaseConfig{
			Type:         "mongodb",
			DbNamePrefix: "db_",
			DefaultDb:    "db",
		},
	}
}

// ── ResolveWorktreesDir ─────────────────────────────────────────────────

func TestResolveWorktreesDir(t *testing.T) {
	tests := []struct {
		name      string
		repo_root string
		cfg       *config.Config
		want      string
	}{
		{
			name:      "nil config uses legacy fallback",
			repo_root: "/Users/dev/apps/myapp",
			cfg:       nil,
			want:      "/Users/dev/apps/myapp-worktrees",
		},
		{
			name:      "config with WorktreesDirAbs",
			repo_root: "/Users/dev/apps/myapp",
			cfg: &config.Config{
				WorktreesDirAbs: "/Users/dev/apps/custom-worktrees",
			},
			want: "/Users/dev/apps/custom-worktrees",
		},
		{
			name:      "config with empty WorktreesDirAbs falls back",
			repo_root: "/Users/dev/apps/myapp",
			cfg: &config.Config{
				WorktreesDirAbs: "",
			},
			want: "/Users/dev/apps/myapp-worktrees",
		},
		{
			name:      "repo root at filesystem boundary",
			repo_root: "/project",
			cfg:       nil,
			want:      "/project-worktrees",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveWorktreesDir(tt.repo_root, tt.cfg)
			if got != tt.want {
				t.Errorf("ResolveWorktreesDir(%q, cfg) = %q, want %q", tt.repo_root, got, tt.want)
			}
		})
	}
}

// ── SortWorktrees ───────────────────────────────────────────────────────

func TestSortWorktrees(t *testing.T) {
	worktrees := []Worktree{
		{Name: "no-container", Type: TypeDocker, Running: false, ContainerExists: false},
		{Name: "stopped-docker", Type: TypeDocker, Running: false, ContainerExists: true},
		{Name: "running-docker", Type: TypeDocker, Running: true, ContainerExists: true},
		{Name: "local-wt", Type: TypeLocal, Running: false},
		{Name: "running-local", Type: TypeLocal, Running: true},
	}

	sorted := SortWorktrees(worktrees)

	expected_order := []string{
		"running-docker",
		"running-local",
		"stopped-docker",
		"local-wt",
		"no-container",
	}

	if len(sorted) != len(expected_order) {
		t.Fatalf("expected %d worktrees, got %d", len(expected_order), len(sorted))
	}

	for i, name := range expected_order {
		if sorted[i].Name != name {
			t.Errorf("position %d: expected %q, got %q", i, name, sorted[i].Name)
		}
	}
}

func TestSortWorktreesEmpty(t *testing.T) {
	sorted := SortWorktrees(nil)
	if len(sorted) != 0 {
		t.Errorf("expected empty slice, got %d items", len(sorted))
	}
}

func TestSortWorktreesAllRunning(t *testing.T) {
	worktrees := []Worktree{
		{Name: "docker-b", Type: TypeDocker, Running: true},
		{Name: "docker-a", Type: TypeDocker, Running: true},
		{Name: "local-a", Type: TypeLocal, Running: true},
	}

	sorted := SortWorktrees(worktrees)

	// Docker running before local running
	if sorted[0].Name != "docker-b" || sorted[1].Name != "docker-a" {
		t.Errorf("expected docker worktrees first, got %q, %q", sorted[0].Name, sorted[1].Name)
	}
	if sorted[2].Name != "local-a" {
		t.Errorf("expected local-a last, got %q", sorted[2].Name)
	}
}

// ── resolve_traefik_domain ──────────────────────────────────────────────

func TestResolveTraefikDomain(t *testing.T) {
	tests := []struct {
		name   string
		routes []traefik_route
		alias  string
		want   string
	}{
		{
			name:   "empty routes",
			routes: []traefik_route{},
			alias:  "test",
			want:   "",
		},
		{
			name:   "single route returns its domain",
			routes: []traefik_route{{domain: "test.example.com", name: "test"}},
			alias:  "test",
			want:   "test.example.com",
		},
		{
			name: "multiple routes prefers non-matching name",
			routes: []traefik_route{
				{domain: "test.auto.com", name: "test"},
				{domain: "custom.example.com", name: "custom"},
			},
			alias: "test",
			want:  "custom.example.com",
		},
		{
			name: "multiple routes all matching alias returns first",
			routes: []traefik_route{
				{domain: "first.example.com", name: "test"},
				{domain: "second.example.com", name: "test"},
			},
			alias: "test",
			want:  "first.example.com",
		},
		{
			name: "multiple routes none matching alias returns first non-match",
			routes: []traefik_route{
				{domain: "alpha.example.com", name: "alpha"},
				{domain: "beta.example.com", name: "beta"},
			},
			alias: "test",
			want:  "alpha.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolve_traefik_domain(tt.routes, tt.alias)
			if got != tt.want {
				t.Errorf("resolve_traefik_domain(%v, %q) = %q, want %q", tt.routes, tt.alias, got, tt.want)
			}
		})
	}
}

// ── read_env_file ───────────────────────────────────────────────────────

func TestReadEnvFile(t *testing.T) {
	dir := tmp_dir(t)
	write_file(t, dir, ".env.worktree", `WORKTREE_ALIAS=my-feature
WORKTREE_HOST_BUILD=true
WORKTREE_PORT_OFFSET=42
MYAPP_MONGO_URL=mongodb://localhost:27017/db_my_feature
EMPTY_VAR=
# COMMENTED_LINE=no
`)

	tests := []struct {
		name     string
		key      string
		want     string
	}{
		{"reads alias", "WORKTREE_ALIAS", "my-feature"},
		{"reads host build", "WORKTREE_HOST_BUILD", "true"},
		{"reads numeric value", "WORKTREE_PORT_OFFSET", "42"},
		{"reads mongo url", "MYAPP_MONGO_URL", "mongodb://localhost:27017/db_my_feature"},
		{"empty value", "EMPTY_VAR", ""},
		{"missing key returns empty", "NONEXISTENT", ""},
		{"does not read comments", "COMMENTED_LINE", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := read_env_file(dir, ".env.worktree", tt.key)
			if got != tt.want {
				t.Errorf("read_env_file(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestReadEnvFileMissingFile(t *testing.T) {
	dir := tmp_dir(t)
	got := read_env_file(dir, ".env.worktree", "ANYTHING")
	if got != "" {
		t.Errorf("expected empty for missing file, got %q", got)
	}
}

func TestReadEnvFileCustomFilename(t *testing.T) {
	dir := tmp_dir(t)
	write_file(t, dir, ".env.custom", "MY_KEY=custom_value\n")

	got := read_env_file(dir, ".env.custom", "MY_KEY")
	if got != "custom_value" {
		t.Errorf("expected 'custom_value', got %q", got)
	}
}

func TestReadEnvFileWhitespace(t *testing.T) {
	dir := tmp_dir(t)
	write_file(t, dir, ".env.worktree", "  MY_KEY=trimmed  \n")

	got := read_env_file(dir, ".env.worktree", "MY_KEY")
	if got != "trimmed" {
		t.Errorf("expected 'trimmed', got %q", got)
	}
}

// ── read_container_name ─────────────────────────────────────────────────

func TestReadContainerName(t *testing.T) {
	dir := tmp_dir(t)
	write_file(t, dir, "docker-compose.worktree.yml", `version: "3"
services:
  app:
    container_name: myapp-my-feature
    image: myapp:latest
`)

	got := read_container_name(dir)
	if got != "myapp-my-feature" {
		t.Errorf("expected 'myapp-my-feature', got %q", got)
	}
}

func TestReadContainerNameNoFile(t *testing.T) {
	dir := tmp_dir(t)
	got := read_container_name(dir)
	if got != "" {
		t.Errorf("expected empty for missing file, got %q", got)
	}
}

func TestReadContainerNameNoMatch(t *testing.T) {
	dir := tmp_dir(t)
	write_file(t, dir, "docker-compose.worktree.yml", `version: "3"
services:
  app:
    image: myapp:latest
`)

	got := read_container_name(dir)
	if got != "" {
		t.Errorf("expected empty when no container_name, got %q", got)
	}
}

// ── read_service_mode ───────────────────────────────────────────────────

func TestReadServiceMode(t *testing.T) {
	tests := []struct {
		name    string
		compose string
		cfg     *config.Config
		want    string
	}{
		{
			name:    "reads mode from compose file, nil config",
			compose: "environment:\n  - WORKTREE_SERVICES=minimal\n",
			cfg:     nil,
			want:    "minimal",
		},
		{
			name:    "reads mode from compose file with config",
			compose: "environment:\n  - WORKTREE_SERVICES=full\n",
			cfg:     test_config(),
			want:    "full",
		},
		{
			name:    "returns default mode when no compose file",
			compose: "",
			cfg:     nil,
			want:    "default",
		},
		{
			name:    "returns config default mode when no compose",
			compose: "",
			cfg: func() *config.Config {
				c := test_config()
				c.Services.DefaultMode = "minimal"
				return c
			}(),
			want: "minimal",
		},
		{
			name:    "returns default when mode not found in compose",
			compose: "environment:\n  - OTHER_VAR=value\n",
			cfg:     nil,
			want:    "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := tmp_dir(t)
			if tt.compose != "" {
				write_file(t, dir, "docker-compose.worktree.yml", tt.compose)
			}
			got := read_service_mode(dir, tt.cfg)
			if got != tt.want {
				t.Errorf("read_service_mode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadServiceModeCustomVar(t *testing.T) {
	dir := tmp_dir(t)
	write_file(t, dir, "docker-compose.worktree.yml", "environment:\n  - CUSTOM_SERVICES=special\n")

	cfg := test_config()
	cfg.Env.WorktreeVars["services"] = "CUSTOM_SERVICES"

	got := read_service_mode(dir, cfg)
	if got != "special" {
		t.Errorf("expected 'special', got %q", got)
	}
}

// ── read_offset ─────────────────────────────────────────────────────────

func TestReadOffset(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		compose string
		cfg     *config.Config
		want    int
	}{
		{
			name: "reads WORKTREE_HOST_PORT_OFFSET",
			env:  "WORKTREE_HOST_PORT_OFFSET=150\n",
			cfg:  nil,
			want: 150,
		},
		{
			name: "reads WORKTREE_PORT_OFFSET when no host offset",
			env:  "WORKTREE_PORT_OFFSET=200\n",
			cfg:  nil,
			want: 200,
		},
		{
			name: "reads WORKTREE_PORT_BASE and subtracts port_base",
			env:  "WORKTREE_PORT_BASE=3500\n",
			cfg:  nil,
			want: 500, // 3500 - 3000
		},
		{
			name: "prefers host offset over regular offset",
			env:  "WORKTREE_HOST_PORT_OFFSET=100\nWORKTREE_PORT_OFFSET=200\n",
			cfg:  nil,
			want: 100,
		},
		{
			name:    "falls back to compose port parsing",
			env:     "",
			compose: "ports:\n  - \"3150:3001\"\n",
			cfg:     nil,
			want:    149, // 3150 - 3001
		},
		{
			name:    "returns 0 when no env or compose",
			env:     "",
			compose: "",
			cfg:     nil,
			want:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := tmp_dir(t)
			if tt.env != "" {
				write_file(t, dir, ".env.worktree", tt.env)
			}
			if tt.compose != "" {
				write_file(t, dir, "docker-compose.worktree.yml", tt.compose)
			}
			got := read_offset(dir, tt.cfg)
			if got != tt.want {
				t.Errorf("read_offset() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestReadOffsetWithConfig(t *testing.T) {
	dir := tmp_dir(t)
	cfg := test_config()

	// Config has ports web:3000, api:3001 — lowest is 3000
	write_file(t, dir, ".env.worktree", "WORKTREE_PORT_BASE=3064\n")

	got := read_offset(dir, cfg)
	// 3064 - 3000 (lowest port) = 64
	if got != 64 {
		t.Errorf("expected offset 64, got %d", got)
	}
}

func TestReadOffsetComposeFallbackWithConfig(t *testing.T) {
	dir := tmp_dir(t)
	cfg := test_config()

	// No env file. Compose has "3064:3000" mapping (primary port is web:3000)
	write_file(t, dir, "docker-compose.worktree.yml", `ports:
  - "3064:3000"
`)

	got := read_offset(dir, cfg)
	// 3064 - 3000 (primary port) = 64
	if got != 64 {
		t.Errorf("expected offset 64, got %d", got)
	}
}

func TestReadOffsetCustomEnvFilename(t *testing.T) {
	dir := tmp_dir(t)
	cfg := test_config()
	cfg.Env.Filename = ".env.custom"

	write_file(t, dir, ".env.custom", "WORKTREE_PORT_OFFSET=77\n")

	got := read_offset(dir, cfg)
	if got != 77 {
		t.Errorf("expected offset 77, got %d", got)
	}
}

// ── read_branch ─────────────────────────────────────────────────────────

func TestReadBranch(t *testing.T) {
	// Set up a fake worktree .git file pointing to a gitdir with a HEAD
	dir := tmp_dir(t)
	gitdir := mkdir(t, dir, ".fake-gitdir")

	// .git file (worktree style) points to the gitdir
	write_file(t, dir, ".git", "gitdir: "+gitdir+"\n")

	// HEAD in gitdir points to a branch
	write_file(t, gitdir, "HEAD", "ref: refs/heads/feat/my-feature\n")

	got := read_branch(dir)
	if got != "feat/my-feature" {
		t.Errorf("expected 'feat/my-feature', got %q", got)
	}
}

func TestReadBranchDetachedHead(t *testing.T) {
	dir := tmp_dir(t)
	gitdir := mkdir(t, dir, ".fake-gitdir")

	write_file(t, dir, ".git", "gitdir: "+gitdir+"\n")
	write_file(t, gitdir, "HEAD", "abc12345deadbeef1234567890abcdef12345678\n")

	got := read_branch(dir)
	// Should return first 8 chars of the SHA
	if got != "abc12345" {
		t.Errorf("expected 'abc12345', got %q", got)
	}
}

func TestReadBranchRelativeGitdir(t *testing.T) {
	dir := tmp_dir(t)
	gitdir := mkdir(t, dir, "relative-gitdir")

	write_file(t, dir, ".git", "gitdir: relative-gitdir\n")
	write_file(t, gitdir, "HEAD", "ref: refs/heads/fix/bug-123\n")

	got := read_branch(dir)
	if got != "fix/bug-123" {
		t.Errorf("expected 'fix/bug-123', got %q", got)
	}
}

func TestReadBranchNoGitFile(t *testing.T) {
	dir := tmp_dir(t)
	got := read_branch(dir)
	if got != "" {
		t.Errorf("expected empty for no .git file, got %q", got)
	}
}

func TestReadBranchShortSHA(t *testing.T) {
	dir := tmp_dir(t)
	gitdir := mkdir(t, dir, ".fake-gitdir")

	write_file(t, dir, ".git", "gitdir: "+gitdir+"\n")
	// SHA shorter than 8 chars
	write_file(t, gitdir, "HEAD", "abc123\n")

	got := read_branch(dir)
	if got != "abc123" {
		t.Errorf("expected 'abc123', got %q", got)
	}
}

// ── build_traefik_port_map ──────────────────────────────────────────────

func TestBuildTraefikPortMap(t *testing.T) {
	dir := tmp_dir(t)
	traefik_dir := mkdir(t, dir, "traefik-dynamic")

	write_file(t, traefik_dir, "my-feature.yml", `http:
  routers:
    my-feature:
      rule: Host(`+"`"+`my-feature.dev.local`+"`"+`)
  services:
    my-feature:
      loadBalancer:
        servers:
          - url: http://host.docker.internal:3064
`)

	write_file(t, traefik_dir, "other.yml", `http:
  routers:
    other:
      rule: Host(`+"`"+`other.dev.local`+"`"+`)
  services:
    other:
      loadBalancer:
        servers:
          - url: http://host.docker.internal:3100
`)

	// Also include a non-yml file that should be ignored
	write_file(t, traefik_dir, "README.md", "This should be ignored")
	// And a directory that should be ignored
	mkdir(t, traefik_dir, "subdir")

	cfg := &config.Config{
		ProxyDynamicDir: traefik_dir,
	}

	result := build_traefik_port_map(dir, cfg)

	if result == nil {
		t.Fatal("expected non-nil result")
	}

	routes_3064, ok := result[3064]
	if !ok {
		t.Fatal("expected routes for port 3064")
	}
	if len(routes_3064) != 1 {
		t.Fatalf("expected 1 route for port 3064, got %d", len(routes_3064))
	}
	if routes_3064[0].domain != "my-feature.dev.local" {
		t.Errorf("expected domain 'my-feature.dev.local', got %q", routes_3064[0].domain)
	}
	if routes_3064[0].name != "my-feature" {
		t.Errorf("expected name 'my-feature', got %q", routes_3064[0].name)
	}

	routes_3100, ok := result[3100]
	if !ok {
		t.Fatal("expected routes for port 3100")
	}
	if routes_3100[0].domain != "other.dev.local" {
		t.Errorf("expected domain 'other.dev.local', got %q", routes_3100[0].domain)
	}
}

func TestBuildTraefikPortMapNoConfig(t *testing.T) {
	result := build_traefik_port_map("/nonexistent", nil)
	if result != nil {
		t.Errorf("expected nil for nil config, got %v", result)
	}
}

func TestBuildTraefikPortMapEmptyDir(t *testing.T) {
	cfg := &config.Config{
		ProxyDynamicDir: "",
	}
	result := build_traefik_port_map("/nonexistent", cfg)
	if result != nil {
		t.Errorf("expected nil for empty ProxyDynamicDir, got %v", result)
	}
}

func TestBuildTraefikPortMapNonexistentDir(t *testing.T) {
	cfg := &config.Config{
		ProxyDynamicDir: "/nonexistent/path/to/traefik",
	}
	result := build_traefik_port_map("/nonexistent", cfg)
	if result != nil {
		t.Errorf("expected nil for nonexistent dir, got %v", result)
	}
}

func TestBuildTraefikPortMapMultipleRoutesOnePort(t *testing.T) {
	dir := tmp_dir(t)
	traefik_dir := mkdir(t, dir, "traefik")

	// Two configs pointing to the same port
	write_file(t, traefik_dir, "auto.yml", `http:
  routers:
    auto:
      rule: Host(`+"`"+`auto.dev.local`+"`"+`)
  services:
    auto:
      loadBalancer:
        servers:
          - url: http://host.docker.internal:3064
`)

	write_file(t, traefik_dir, "custom.yml", `http:
  routers:
    custom:
      rule: Host(`+"`"+`custom.example.com`+"`"+`)
  services:
    custom:
      loadBalancer:
        servers:
          - url: http://host.docker.internal:3064
`)

	cfg := &config.Config{
		ProxyDynamicDir: traefik_dir,
	}

	result := build_traefik_port_map(dir, cfg)

	routes, ok := result[3064]
	if !ok {
		t.Fatal("expected routes for port 3064")
	}
	if len(routes) != 2 {
		t.Errorf("expected 2 routes for port 3064, got %d", len(routes))
	}
}

// ── file_exists ─────────────────────────────────────────────────────────

func TestFileExists(t *testing.T) {
	dir := tmp_dir(t)
	write_file(t, dir, "exists.txt", "content")

	if !file_exists(filepath.Join(dir, "exists.txt")) {
		t.Error("expected file to exist")
	}
	if file_exists(filepath.Join(dir, "nope.txt")) {
		t.Error("expected file to not exist")
	}
}

// ── Discover (integration-style with temp filesystem) ───────────────────

func TestDiscoverEmptyDir(t *testing.T) {
	dir := tmp_dir(t)
	results := Discover(dir, nil, nil)
	if len(results) != 0 {
		t.Errorf("expected 0 worktrees, got %d", len(results))
	}
}

func TestDiscoverNonexistentDir(t *testing.T) {
	results := Discover("/nonexistent/path", nil, nil)
	if len(results) != 0 {
		t.Errorf("expected 0 worktrees, got %d", len(results))
	}
}

func TestDiscoverSkipsFiles(t *testing.T) {
	dir := tmp_dir(t)
	write_file(t, dir, "not-a-dir.txt", "just a file")

	results := Discover(dir, nil, nil)
	if len(results) != 0 {
		t.Errorf("expected 0 worktrees (files skipped), got %d", len(results))
	}
}

func TestDiscoverSkipsDirsWithoutDockerOrGit(t *testing.T) {
	dir := tmp_dir(t)
	mkdir(t, dir, "random-dir")
	write_file(t, filepath.Join(dir, "random-dir"), "somefile.txt", "content")

	results := Discover(dir, nil, nil)
	if len(results) != 0 {
		t.Errorf("expected 0 worktrees (no .git or docker files), got %d", len(results))
	}
}

func TestDiscoverLocalWorktree(t *testing.T) {
	dir := tmp_dir(t)

	// Create a worktree with only a .git file (local, no docker)
	wt_dir := mkdir(t, dir, "my-local-wt")
	gitdir := mkdir(t, wt_dir, ".fake-gitdir")
	write_file(t, wt_dir, ".git", "gitdir: "+gitdir+"\n")
	write_file(t, gitdir, "HEAD", "ref: refs/heads/feat/local-only\n")

	results := Discover(dir, nil, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(results))
	}

	wt := results[0]
	if wt.Type != TypeLocal {
		t.Errorf("expected TypeLocal, got %q", wt.Type)
	}
	if wt.Name != "my-local-wt" {
		t.Errorf("expected name 'my-local-wt', got %q", wt.Name)
	}
	if wt.Alias != "my-local-wt" {
		t.Errorf("expected alias to equal name, got %q", wt.Alias)
	}
	if wt.Branch != "feat/local-only" {
		t.Errorf("expected branch 'feat/local-only', got %q", wt.Branch)
	}
}

func TestDiscoverDockerWorktreeNoConfig(t *testing.T) {
	dir := tmp_dir(t)

	wt_dir := mkdir(t, dir, "my-docker-wt")
	write_file(t, wt_dir, ".env.worktree", "WORKTREE_ALIAS=my-feat\nWORKTREE_PORT_OFFSET=64\n")
	write_file(t, wt_dir, "docker-compose.worktree.yml", `version: "3"
services:
  app:
    container_name: skulabs-my-feat
    image: myapp:latest
    ports:
      - "3065:3001"
    environment:
      - WORKTREE_SERVICES=default
`)

	// Add a .git for branch detection
	gitdir := mkdir(t, wt_dir, ".fake-gitdir")
	write_file(t, wt_dir, ".git", "gitdir: "+gitdir+"\n")
	write_file(t, gitdir, "HEAD", "ref: refs/heads/feat/docker-wt\n")

	results := Discover(dir, nil, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(results))
	}

	wt := results[0]
	if wt.Type != TypeDocker {
		t.Errorf("expected TypeDocker, got %q", wt.Type)
	}
	if wt.Alias != "my-feat" {
		t.Errorf("expected alias 'my-feat', got %q", wt.Alias)
	}
	if wt.Container != "skulabs-my-feat" {
		t.Errorf("expected container 'skulabs-my-feat', got %q", wt.Container)
	}
	if wt.Mode != "default" {
		t.Errorf("expected mode 'default', got %q", wt.Mode)
	}
	if wt.Offset != 64 {
		t.Errorf("expected offset 64, got %d", wt.Offset)
	}
	if wt.Branch != "feat/docker-wt" {
		t.Errorf("expected branch 'feat/docker-wt', got %q", wt.Branch)
	}
	if wt.Domain != "my-feat.localhost" {
		t.Errorf("expected domain 'my-feat.localhost', got %q", wt.Domain)
	}
	// DB name: no config, no mongo url -> db_{safe_alias}
	if wt.DBName != "db_my_feat" {
		t.Errorf("expected db name 'db_my_feat', got %q", wt.DBName)
	}
}

func TestDiscoverDockerWorktreeWithConfig(t *testing.T) {
	dir := tmp_dir(t)
	cfg := test_config()

	wt_dir := mkdir(t, dir, "my-wt")
	write_file(t, wt_dir, ".env.worktree", `WORKTREE_ALIAS=my-wt
WORKTREE_PORT_OFFSET=50
WORKTREE_HOST_BUILD=true
MYAPP_MONGO_URL=mongodb://localhost:27017/db_my_wt
`)
	write_file(t, wt_dir, "docker-compose.worktree.yml", `version: "3"
services:
  app:
    container_name: myapp-my-wt
    environment:
      - WORKTREE_SERVICES=minimal
`)

	results := Discover(dir, nil, cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(results))
	}

	wt := results[0]
	if wt.Type != TypeDocker {
		t.Errorf("expected TypeDocker, got %q", wt.Type)
	}
	if wt.Alias != "my-wt" {
		t.Errorf("expected alias 'my-wt', got %q", wt.Alias)
	}
	if wt.Container != "myapp-my-wt" {
		t.Errorf("expected container 'myapp-my-wt', got %q", wt.Container)
	}
	if wt.HostBuild != true {
		t.Errorf("expected HostBuild=true, got false")
	}
	if wt.Mode != "minimal" {
		t.Errorf("expected mode 'minimal', got %q", wt.Mode)
	}
	if wt.Offset != 50 {
		t.Errorf("expected offset 50, got %d", wt.Offset)
	}
	if wt.DBName != "db_my_wt" {
		t.Errorf("expected db name 'db_my_wt', got %q", wt.DBName)
	}
	// App port = primary (web: 3000) + 50 = 3050
	// No traefik dir configured, so domain should come from config.DomainFor
	if wt.Domain != "my-wt.localhost" {
		t.Errorf("expected domain 'my-wt.localhost', got %q", wt.Domain)
	}
}

func TestDiscoverAliasDefaultsToName(t *testing.T) {
	dir := tmp_dir(t)

	// Docker worktree without WORKTREE_ALIAS in env
	wt_dir := mkdir(t, dir, "no-alias-wt")
	write_file(t, wt_dir, ".env.worktree", "WORKTREE_PORT_OFFSET=10\n")
	write_file(t, wt_dir, "docker-compose.worktree.yml", `version: "3"
services:
  app:
    container_name: myapp-no-alias-wt
`)

	results := Discover(dir, nil, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(results))
	}

	if results[0].Alias != "no-alias-wt" {
		t.Errorf("expected alias to default to name 'no-alias-wt', got %q", results[0].Alias)
	}
}

func TestDiscoverContainerNameFallbackConfig(t *testing.T) {
	dir := tmp_dir(t)
	cfg := test_config()

	// No compose file, just env file — container from config.ContainerName
	wt_dir := mkdir(t, dir, "env-only")
	write_file(t, wt_dir, ".env.worktree", "WORKTREE_ALIAS=env-only\n")

	results := Discover(dir, nil, cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(results))
	}

	// Config: ContainerName("env-only") = "myapp-env-only"
	if results[0].Container != "myapp-env-only" {
		t.Errorf("expected container 'myapp-env-only', got %q", results[0].Container)
	}
}

func TestDiscoverContainerNameFallbackNoConfig(t *testing.T) {
	dir := tmp_dir(t)

	// No compose file, just env file, no config — container = name
	wt_dir := mkdir(t, dir, "simple-wt")
	write_file(t, wt_dir, ".env.worktree", "WORKTREE_ALIAS=simple\n")

	results := Discover(dir, nil, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(results))
	}

	if results[0].Container != "simple-wt" {
		t.Errorf("expected container 'simple-wt' (dir name), got %q", results[0].Container)
	}
}

func TestDiscoverPreservesRuntimeState(t *testing.T) {
	dir := tmp_dir(t)

	wt_dir := mkdir(t, dir, "stateful-wt")
	write_file(t, wt_dir, ".env.worktree", "WORKTREE_ALIAS=stateful\n")
	write_file(t, wt_dir, "docker-compose.worktree.yml", `services:
  app:
    container_name: myapp-stateful
`)

	full_path := filepath.Join(dir, "stateful-wt")
	existing := []Worktree{
		{
			Path:            full_path,
			Running:         true,
			ContainerExists: true,
			Health:          "healthy",
			Started:         "2025-01-01T00:00:00Z",
			Uptime:          "2h",
			CPU:             "5.2%",
			Mem:             "128MiB",
			MemPct:          "3.5%",
		},
	}

	results := Discover(dir, existing, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(results))
	}

	wt := results[0]
	if !wt.Running {
		t.Error("expected Running to be preserved as true")
	}
	if !wt.ContainerExists {
		t.Error("expected ContainerExists to be preserved as true")
	}
	if wt.Health != "healthy" {
		t.Errorf("expected Health 'healthy', got %q", wt.Health)
	}
	if wt.Started != "2025-01-01T00:00:00Z" {
		t.Errorf("expected Started preserved, got %q", wt.Started)
	}
	if wt.Uptime != "2h" {
		t.Errorf("expected Uptime '2h', got %q", wt.Uptime)
	}
	if wt.CPU != "5.2%" {
		t.Errorf("expected CPU '5.2%%', got %q", wt.CPU)
	}
	if wt.Mem != "128MiB" {
		t.Errorf("expected Mem '128MiB', got %q", wt.Mem)
	}
	if wt.MemPct != "3.5%" {
		t.Errorf("expected MemPct '3.5%%', got %q", wt.MemPct)
	}
}

func TestDiscoverMultipleWorktrees(t *testing.T) {
	dir := tmp_dir(t)

	// Docker worktree
	docker_wt := mkdir(t, dir, "docker-wt")
	write_file(t, docker_wt, ".env.worktree", "WORKTREE_ALIAS=docker\n")
	write_file(t, docker_wt, "docker-compose.worktree.yml", `services:
  app:
    container_name: app-docker
`)

	// Local worktree
	local_wt := mkdir(t, dir, "local-wt")
	gitdir := mkdir(t, local_wt, ".fake-gitdir")
	write_file(t, local_wt, ".git", "gitdir: "+gitdir+"\n")
	write_file(t, gitdir, "HEAD", "ref: refs/heads/main\n")

	// Skip: no docker or git
	random := mkdir(t, dir, "random-dir")
	write_file(t, random, "file.txt", "nothing")

	results := Discover(dir, nil, nil)
	if len(results) != 2 {
		t.Fatalf("expected 2 worktrees, got %d", len(results))
	}

	// Verify both types exist
	var found_docker, found_local bool
	for _, wt := range results {
		if wt.Type == TypeDocker {
			found_docker = true
		}
		if wt.Type == TypeLocal {
			found_local = true
		}
	}
	if !found_docker {
		t.Error("expected a docker worktree")
	}
	if !found_local {
		t.Error("expected a local worktree")
	}
}

func TestDiscoverDbNameFromMongoUrl(t *testing.T) {
	dir := tmp_dir(t)
	cfg := test_config()

	wt_dir := mkdir(t, dir, "with-mongo")
	write_file(t, wt_dir, ".env.worktree", `WORKTREE_ALIAS=mongo-test
MYAPP_MONGO_URL=mongodb://localhost:27017/custom_db_name
`)
	write_file(t, wt_dir, "docker-compose.worktree.yml", `services:
  app:
    container_name: myapp-mongo-test
`)

	results := Discover(dir, nil, cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(results))
	}

	if results[0].DBName != "custom_db_name" {
		t.Errorf("expected db name 'custom_db_name', got %q", results[0].DBName)
	}
}

func TestDiscoverDbNameFromMongoUrlTrailingSlash(t *testing.T) {
	dir := tmp_dir(t)
	cfg := test_config()

	wt_dir := mkdir(t, dir, "trailing-slash")
	write_file(t, wt_dir, ".env.worktree", `WORKTREE_ALIAS=trailing
MYAPP_MONGO_URL=mongodb://localhost:27017/
`)
	write_file(t, wt_dir, "docker-compose.worktree.yml", `services:
  app:
    container_name: myapp-trailing
`)

	results := Discover(dir, nil, cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(results))
	}

	// Trailing slash → empty db name → falls back to DefaultDb
	if results[0].DBName != "db" {
		t.Errorf("expected db name 'db' (DefaultDb fallback), got %q", results[0].DBName)
	}
}

func TestDiscoverDbNameNoMongoNoConfig(t *testing.T) {
	dir := tmp_dir(t)

	wt_dir := mkdir(t, dir, "no-mongo")
	write_file(t, wt_dir, ".env.worktree", "WORKTREE_ALIAS=my-feat\n")
	write_file(t, wt_dir, "docker-compose.worktree.yml", `services:
  app:
    container_name: app-my-feat
`)

	results := Discover(dir, nil, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(results))
	}

	// No config, no mongo URL → db_{safe_alias}
	if results[0].DBName != "db_my_feat" {
		t.Errorf("expected db name 'db_my_feat', got %q", results[0].DBName)
	}
}

func TestDiscoverDbNameSanitizesAlias(t *testing.T) {
	dir := tmp_dir(t)

	wt_dir := mkdir(t, dir, "special-chars")
	write_file(t, wt_dir, ".env.worktree", "WORKTREE_ALIAS=my-feat.2024\n")
	write_file(t, wt_dir, "docker-compose.worktree.yml", `services:
  app:
    container_name: app-special
`)

	results := Discover(dir, nil, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(results))
	}

	// Dashes and dots → underscores
	if results[0].DBName != "db_my_feat_2024" {
		t.Errorf("expected db name 'db_my_feat_2024', got %q", results[0].DBName)
	}
}

func TestDiscoverHostBuild(t *testing.T) {
	dir := tmp_dir(t)

	wt_dir := mkdir(t, dir, "host-build-wt")
	write_file(t, wt_dir, ".env.worktree", "WORKTREE_HOST_BUILD=true\n")
	write_file(t, wt_dir, "docker-compose.worktree.yml", `services:
  app:
    container_name: app-host
`)

	results := Discover(dir, nil, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(results))
	}

	if !results[0].HostBuild {
		t.Error("expected HostBuild=true")
	}
}

func TestDiscoverHostBuildFalse(t *testing.T) {
	dir := tmp_dir(t)

	wt_dir := mkdir(t, dir, "no-host-build")
	write_file(t, wt_dir, ".env.worktree", "WORKTREE_HOST_BUILD=false\n")
	write_file(t, wt_dir, "docker-compose.worktree.yml", `services:
  app:
    container_name: app-no-host
`)

	results := Discover(dir, nil, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(results))
	}

	if results[0].HostBuild {
		t.Error("expected HostBuild=false")
	}
}

func TestDiscoverCustomEnvFilename(t *testing.T) {
	dir := tmp_dir(t)
	cfg := test_config()
	cfg.Env.Filename = ".env.custom"

	wt_dir := mkdir(t, dir, "custom-env")
	write_file(t, wt_dir, ".env.custom", "WORKTREE_ALIAS=custom-alias\nWORKTREE_PORT_OFFSET=33\n")
	write_file(t, wt_dir, "docker-compose.worktree.yml", `services:
  app:
    container_name: myapp-custom-alias
`)

	results := Discover(dir, nil, cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(results))
	}

	if results[0].Alias != "custom-alias" {
		t.Errorf("expected alias 'custom-alias', got %q", results[0].Alias)
	}
	if results[0].Offset != 33 {
		t.Errorf("expected offset 33, got %d", results[0].Offset)
	}
}

func TestDiscoverLANDomain(t *testing.T) {
	dir := tmp_dir(t)
	cfg := test_config()

	wt_dir := mkdir(t, dir, "lan-wt")
	write_file(t, wt_dir, ".env.worktree", `WORKTREE_ALIAS=lan-wt
MYAPP_LAN_DOMAIN=lan-wt.192.168.1.100.nip.io
`)
	write_file(t, wt_dir, "docker-compose.worktree.yml", `services:
  app:
    container_name: myapp-lan-wt
`)

	results := Discover(dir, nil, cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(results))
	}

	wt := results[0]
	if wt.LANDomain != "lan-wt.192.168.1.100.nip.io" {
		t.Errorf("expected LANDomain, got %q", wt.LANDomain)
	}
	// When LAN domain is set, Domain should equal LANDomain
	if wt.Domain != "lan-wt.192.168.1.100.nip.io" {
		t.Errorf("expected Domain to use LAN domain, got %q", wt.Domain)
	}
}

func TestDiscoverSharedCompose(t *testing.T) {
	dir := tmp_dir(t)
	cfg := test_config()
	cfg.Docker.ComposeStrategy = "shared"
	cfg.ComposeFileAbs = "/some/path/docker-compose.dev.yml"

	wt_dir := mkdir(t, dir, "shared-wt")
	write_file(t, wt_dir, ".env.worktree", `WORKTREE_ALIAS=shared-wt
BRANCH_SLUG=shared-wt
`)

	results := Discover(dir, nil, cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(results))
	}

	// For shared compose: container = name-slug-primary
	// primary = "web" (from config)
	if results[0].Container != "myapp-shared-wt-web" {
		t.Errorf("expected container 'myapp-shared-wt-web', got %q", results[0].Container)
	}
}

func TestDiscoverAppPort(t *testing.T) {
	dir := tmp_dir(t)
	cfg := test_config()

	wt_dir := mkdir(t, dir, "port-wt")
	write_file(t, wt_dir, ".env.worktree", `WORKTREE_ALIAS=port-wt
WORKTREE_PORT_OFFSET=100
`)
	write_file(t, wt_dir, "docker-compose.worktree.yml", `services:
  app:
    container_name: myapp-port-wt
`)

	results := Discover(dir, nil, cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(results))
	}

	// With config: PrimaryPort (web: 3000) + offset 100 = 3100
	// We can't check app_port directly (it's local), but Domain is derived from it
	// when no traefik or LAN. Domain falls back to config.DomainFor.
	if results[0].Domain != "port-wt.localhost" {
		t.Errorf("expected domain 'port-wt.localhost', got %q", results[0].Domain)
	}
}

func TestDiscoverContainerPrefix(t *testing.T) {
	dir := tmp_dir(t)
	cfg := test_config()

	wt_dir := mkdir(t, dir, "prefix-wt")
	write_file(t, wt_dir, ".env.worktree", "WORKTREE_ALIAS=prefix-wt\n")
	write_file(t, wt_dir, "docker-compose.worktree.yml", `services:
  app:
    container_name: myapp-prefix-wt
`)

	results := Discover(dir, nil, cfg)
	if len(results) != 1 {
		t.Fatalf("expected 1 worktree, got %d", len(results))
	}

	// container = "myapp-prefix-wt", prefix = "myapp-"
	// container_alias = TrimPrefix("myapp-prefix-wt", "myapp-") = "prefix-wt"
	// This is used internally for traefik resolution, hard to verify directly
	// but we can verify the container was parsed correctly
	if results[0].Container != "myapp-prefix-wt" {
		t.Errorf("expected container 'myapp-prefix-wt', got %q", results[0].Container)
	}
}
