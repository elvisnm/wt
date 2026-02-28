// Package config loads and provides access to the workflow.config.js configuration.
//
// It executes `node -e "..."` to evaluate the JavaScript config file and parse
// the resulting JSON into Go structs, ensuring identical behavior to the Node.js loader.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const ConfigFilename = "workflow.config.js"

// Config is the top-level workflow configuration.
type Config struct {
	Name string `json:"name"`

	Repo       RepoConfig       `json:"repo"`
	Docker     DockerConfig     `json:"docker"`
	Services   ServicesConfig   `json:"services"`
	PortOffset PortOffsetConfig `json:"portOffset"`
	Database   *DatabaseConfig  `json:"database"`
	Redis      *RedisConfig     `json:"redis"`
	Env        EnvConfig        `json:"env"`
	Features   FeaturesConfig   `json:"features"`
	Dash       DashConfig       `json:"dash"`
	Git        GitConfig         `json:"git"`
	Paths      PathsConfig      `json:"paths"`

	// Resolved paths (set by Load, not from JSON)
	RepoRoot          string `json:"-"`
	ConfigPath        string `json:"-"`
	WorktreesDirAbs   string `json:"-"`
	ComposePathAbs    string `json:"-"`
	ProxyDynamicDir   string `json:"-"`
	ComposeFileAbs    string `json:"-"`
}

type RepoConfig struct {
	WorktreesDir   string   `json:"worktreesDir"`
	BranchPrefixes []string `json:"branchPrefixes"`
	BaseRefs       []string `json:"baseRefs"`
}

type DockerConfig struct {
	BaseImage       string             `json:"baseImage"`
	ComposeStrategy string             `json:"composeStrategy"`
	ComposeFile     string             `json:"composeFile"`
	Generate        *DockerGenConfig   `json:"generate"`
	SharedInfra     SharedInfraConfig  `json:"sharedInfra"`
	Proxy           ProxyConfig        `json:"proxy"`
	EnvFiles        []string           `json:"envFiles"`
}

type DockerGenConfig struct {
	ContainerWorkdir string             `json:"containerWorkdir"`
	Entrypoint       string             `json:"entrypoint"`
	ExtraMounts      []string           `json:"extraMounts"`
	ExtraEnv         map[string]string  `json:"extraEnv"`
	OverrideFiles    []OverrideFile     `json:"overrideFiles"`
}

type OverrideFile struct {
	Src string `json:"src"`
	Dst string `json:"dst"`
}

type SharedInfraConfig struct {
	Network     string `json:"network"`
	ComposePath string `json:"composePath"`
}

type ProxyConfig struct {
	Type           string `json:"type"`
	DynamicDir     string `json:"dynamicDir"`
	DomainTemplate string `json:"domainTemplate"`
}

type ServicesConfig struct {
	Ports       map[string]int      `json:"ports"`
	Modes       map[string][]string `json:"modes"`
	DefaultMode string              `json:"defaultMode"`
	Primary     string              `json:"primary"`
	QuickLinks  []QuickLink         `json:"quickLinks"`
}

type QuickLink struct {
	Label      string `json:"label"`
	Service    string `json:"service"`
	PathPrefix string `json:"pathPrefix"`
}

type PortOffsetConfig struct {
	Algorithm   string `json:"algorithm"`
	Min         int    `json:"min"`
	Range       int    `json:"range"`
	AutoResolve bool   `json:"autoResolve"`
}

type DatabaseConfig struct {
	Type         string `json:"type"`
	Host         string `json:"host"`
	ContainerHost string `json:"containerHost"`
	Port         int    `json:"port"`
	DefaultDb    string `json:"defaultDb"`
	ReplicaSet   string `json:"replicaSet"`
	DbNamePrefix string `json:"dbNamePrefix"`
	SeedCommand  string `json:"seedCommand"`
	DropCommand  string `json:"dropCommand"`
}

type RedisConfig struct {
	ContainerHost string `json:"containerHost"`
	Port          int    `json:"port"`
}

type EnvConfig struct {
	Prefix       string            `json:"prefix"`
	Filename     string            `json:"filename"`
	Vars         map[string]string `json:"vars"`
	WorktreeVars map[string]string `json:"worktreeVars"`
}

type FeaturesConfig struct {
	HostBuild      bool         `json:"hostBuild"`
	Lan            bool         `json:"lan"`
	Admin          AdminConfig  `json:"admin"`
	AwsCredentials bool         `json:"awsCredentials"`
	Autostop       bool         `json:"autostop"`
	Prune          bool         `json:"prune"`
	ImagesFix      bool         `json:"imagesFix"`
	RebuildBase    bool         `json:"rebuildBase"`
	DevHeap        *int         `json:"devHeap"`
}

type AdminConfig struct {
	Enabled       bool   `json:"enabled"`
	DefaultUserId string `json:"defaultUserId"`
}

type DashConfig struct {
	Commands        map[string]DashCommand `json:"commands"`
	LocalDevCommand string                 `json:"localDevCommand"`
	Services        DashServicesConfig     `json:"services"`
}

type DashCommand struct {
	Label string `json:"label"`
	Cmd   string `json:"cmd"`
}

type DashServicesConfig struct {
	Manager      string             `json:"manager"`      // "pm2" | "static"
	List         []DashServiceEntry `json:"list"`
	RunningCheck string             `json:"runningCheck"` // "pm2" | "devTab"
	Docker       *DashDockerSvc     `json:"docker"`
}

type DashServiceEntry struct {
	Name string `json:"name"`
	Port int    `json:"port"`
}

type DashDockerSvc struct {
	Manager string `json:"manager"`
}

type PathsConfig struct {
	FlowScripts     string `json:"flowScripts"`
	DockerOverrides string `json:"dockerOverrides"`
	BuildScript     string `json:"buildScript"`
}

type GitConfig struct {
	SkipWorktree []string `json:"skipWorktree"`
}

// ── Finding the config ──────────────────────────────────────────────────

// FindConfig walks upward from start_dir looking for workflow.config.js.
// Returns (configPath, repoRoot) or empty strings if not found.
func FindConfig(start_dir string) (string, string) {
	dir, err := filepath.Abs(start_dir)
	if err != nil {
		return "", ""
	}

	for {
		candidate := filepath.Join(dir, ConfigFilename)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break // hit filesystem root
		}
		dir = parent
	}

	return "", ""
}

// ── Loading the config ──────────────────────────────────────────────────

// Load finds and parses workflow.config.js, resolves defaults and paths.
// start_dir is the directory to search from (typically CWD or repo root).
func Load(start_dir string) (*Config, error) {
	config_path, repo_root := FindConfig(start_dir)
	if config_path == "" {
		return nil, fmt.Errorf("could not find %s in %s or any parent directory", ConfigFilename, start_dir)
	}

	return LoadFromPath(config_path, repo_root)
}

// LoadFromPath loads a specific config file with a known repo root.
func LoadFromPath(config_path string, repo_root string) (*Config, error) {
	// Use Node.js to evaluate the JS config and output JSON.
	// This ensures we get identical semantics to the Node.js loader
	// (supports require(), process.env, conditionals, etc.)
	script := fmt.Sprintf(
		`try { const c = require(%q); console.log(JSON.stringify(c)); } catch(e) { console.error(e.message); process.exit(1); }`,
		config_path,
	)

	cmd := exec.Command("node", "-e", script)
	cmd.Dir = repo_root
	output, err := cmd.Output()
	if err != nil {
		if exit_err, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("failed to evaluate %s: %s", config_path, strings.TrimSpace(string(exit_err.Stderr)))
		}
		return nil, fmt.Errorf("failed to run node to evaluate config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(output, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config JSON: %w", err)
	}

	// Validate required fields
	if cfg.Name == "" {
		return nil, fmt.Errorf("%s: \"name\" is required", ConfigFilename)
	}

	cfg.ConfigPath = config_path
	cfg.RepoRoot = repo_root

	// Apply computed defaults
	cfg.applyDefaults()

	// Resolve {PREFIX} templates in env vars
	cfg.resolveEnvTemplates()

	// Resolve paths
	cfg.resolvePaths()

	return &cfg, nil
}

// applyDefaults fills in computed default values for fields not set in the config.
func (c *Config) applyDefaults() {
	// repo.worktreesDir
	if c.Repo.WorktreesDir == "" {
		c.Repo.WorktreesDir = fmt.Sprintf("../%s-worktrees", c.Name)
	}

	// env.prefix
	if c.Env.Prefix == "" {
		c.Env.Prefix = strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(c.Name))
	}

	// env.filename
	if c.Env.Filename == "" {
		c.Env.Filename = ".env.worktree"
	}

	// portOffset defaults
	if c.PortOffset.Algorithm == "" {
		c.PortOffset.Algorithm = "sha256"
	}
	if c.PortOffset.Range == 0 {
		c.PortOffset.Range = 2000
	}
	if c.PortOffset.Min == 0 {
		c.PortOffset.Min = 100
	}

	// database prefix
	if c.Database != nil && c.Database.DbNamePrefix == "" {
		c.Database.DbNamePrefix = "db_"
	}

	// dash.services defaults
	if c.Dash.Services.Manager == "" {
		c.Dash.Services.Manager = "pm2"
	}
	if c.Dash.Services.RunningCheck == "" {
		c.Dash.Services.RunningCheck = "pm2"
	}

	// proxy defaults
	if c.Docker.Proxy.DynamicDir == "" && c.Docker.Proxy.Type == "traefik" {
		c.Docker.Proxy.DynamicDir = "traefik/dynamic"
	}
	if c.Docker.Proxy.DomainTemplate == "" {
		c.Docker.Proxy.DomainTemplate = "{alias}.localhost"
	}
}

// resolveEnvTemplates replaces {PREFIX} in env var name templates with the actual prefix.
func (c *Config) resolveEnvTemplates() {
	if c.Env.Prefix == "" {
		return
	}
	for key, val := range c.Env.Vars {
		c.Env.Vars[key] = strings.ReplaceAll(val, "{PREFIX}", c.Env.Prefix)
	}
}

// resolvePaths converts relative paths in the config to absolute paths.
func (c *Config) resolvePaths() {
	// worktreesDir
	c.WorktreesDirAbs = resolve_path(c.RepoRoot, c.Repo.WorktreesDir)

	// sharedInfra.composePath
	if c.Docker.SharedInfra.ComposePath != "" {
		c.ComposePathAbs = resolve_path(c.RepoRoot, expand_tilde(c.Docker.SharedInfra.ComposePath))
	}

	// proxy dynamicDir (relative to composePath)
	if c.Docker.Proxy.Type == "traefik" && c.ComposePathAbs != "" && c.Docker.Proxy.DynamicDir != "" {
		c.ProxyDynamicDir = filepath.Join(c.ComposePathAbs, c.Docker.Proxy.DynamicDir)
	}

	// composeFile
	if c.Docker.ComposeFile != "" {
		c.ComposeFileAbs = resolve_path(c.RepoRoot, c.Docker.ComposeFile)
	}
}

// ── Derived value helpers ───────────────────────────────────────────────

// ContainerName returns the Docker container name for a worktree alias.
func (c *Config) ContainerName(alias string) string {
	return fmt.Sprintf("%s-%s", c.Name, alias)
}

// VolumePrefix returns the Docker volume name prefix for a worktree alias.
func (c *Config) VolumePrefix(alias string) string {
	return fmt.Sprintf("%s_%s", c.Name, alias)
}

// ContainerFilter returns the Docker filter string to match all project containers.
func (c *Config) ContainerFilter() string {
	return fmt.Sprintf("name=%s-", c.Name)
}

// ContainerPrefix returns just the container name prefix (e.g. "myapp-").
func (c *Config) ContainerPrefix() string {
	return c.Name + "-"
}

// DomainFor returns the domain for a worktree alias using the proxy template.
func (c *Config) DomainFor(alias string) string {
	if c.Docker.Proxy.DomainTemplate == "" {
		return fmt.Sprintf("%s.localhost", alias)
	}
	return strings.ReplaceAll(c.Docker.Proxy.DomainTemplate, "{alias}", alias)
}

// DbName returns the database name for a worktree alias.
func (c *Config) DbName(alias string) string {
	if c.Database == nil || c.Database.Type == "" {
		return ""
	}
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return '_'
	}, alias)
	return c.Database.DbNamePrefix + safe
}

// ServicesForMode returns the service list for a given mode.
// Returns nil if the mode means "all services".
func (c *Config) ServicesForMode(mode string) []string {
	if mode == "" {
		mode = c.Services.DefaultMode
	}
	if mode == "" {
		return nil
	}
	services, ok := c.Services.Modes[mode]
	if !ok {
		return nil
	}
	return services // nil means "all"
}

// PrimaryPort returns the base port of the primary service.
func (c *Config) PrimaryPort() int {
	if c.Services.Primary == "" {
		return 0
	}
	port, ok := c.Services.Ports[c.Services.Primary]
	if !ok {
		return 0
	}
	return port
}

// ComputePorts returns all service ports with the given offset applied.
func (c *Config) ComputePorts(offset int) map[string]int {
	result := make(map[string]int, len(c.Services.Ports))
	for name, base := range c.Services.Ports {
		result[name] = base + offset
	}
	return result
}

// FeatureEnabled checks if a feature flag is enabled.
func (c *Config) FeatureEnabled(name string) bool {
	switch name {
	case "hostBuild":
		return c.Features.HostBuild
	case "lan":
		return c.Features.Lan
	case "admin":
		return c.Features.Admin.Enabled
	case "awsCredentials":
		return c.Features.AwsCredentials
	case "autostop":
		return c.Features.Autostop
	case "prune":
		return c.Features.Prune
	case "imagesFix":
		return c.Features.ImagesFix
	case "rebuildBase":
		return c.Features.RebuildBase
	default:
		return false
	}
}

// EnvVar returns the resolved environment variable name for a logical key.
func (c *Config) EnvVar(key string) string {
	if v, ok := c.Env.Vars[key]; ok {
		return v
	}
	return ""
}

// WorktreeVar returns the worktree-specific env var name for a logical key.
func (c *Config) WorktreeVar(key string) string {
	if v, ok := c.Env.WorktreeVars[key]; ok {
		return v
	}
	return ""
}

// ServiceManager returns the effective service manager for local worktrees.
func (c *Config) ServiceManager() string {
	return c.Dash.Services.Manager
}

// DockerServiceManager returns the effective service manager for Docker containers.
// Falls back to the top-level manager if docker-specific override is not set.
func (c *Config) DockerServiceManager() string {
	if c.Dash.Services.Docker != nil && c.Dash.Services.Docker.Manager != "" {
		return c.Dash.Services.Docker.Manager
	}
	return c.Dash.Services.Manager
}

// ── Utilities ───────────────────────────────────────────────────────────

func resolve_path(base string, rel string) string {
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(base, rel)
}

func expand_tilde(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return filepath.Join(home, p[2:])
	}
	return p
}
