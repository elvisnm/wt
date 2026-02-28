package worktree

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/elvisnm/wt/internal/config"
)

// Discover scans the worktrees directory and returns all found worktrees.
// It reads .env.worktree and docker-compose.worktree.yml for metadata.
// When cfg is non-nil, config-driven values are used instead of hardcoded defaults.
func Discover(worktrees_dir string, existing []Worktree, cfg *config.Config) []Worktree {
	entries, err := os.ReadDir(worktrees_dir)
	if err != nil {
		return []Worktree{}
	}

	existing_map := make(map[string]*Worktree)
	for i := range existing {
		existing_map[existing[i].Path] = &existing[i]
	}

	traefik_port_map := build_traefik_port_map(worktrees_dir, cfg)

	var results []Worktree
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		full_path := filepath.Join(worktrees_dir, entry.Name())
		has_compose := file_exists(filepath.Join(full_path, "docker-compose.worktree.yml"))
		env_filename := ".env.worktree"
		if cfg != nil && cfg.Env.Filename != "" {
			env_filename = cfg.Env.Filename
		}
		has_env := file_exists(filepath.Join(full_path, env_filename))
		has_docker := has_compose || has_env
		has_git := file_exists(filepath.Join(full_path, ".git"))

		if !has_docker && !has_git {
			continue
		}

		name := entry.Name()
		branch := read_branch(full_path)

		if !has_docker {
			results = append(results, Worktree{
				Path:   full_path,
				Name:   name,
				Type:   TypeLocal,
				Alias:  name,
				Branch: branch,
			})
			continue
		}

		// Resolve env var names from config or fall back to hardcoded
		alias_var := "WORKTREE_ALIAS"
		host_build_var := "WORKTREE_HOST_BUILD"
		lan_domain_var := ""
		db_conn_var := ""
		if cfg != nil {
			if v := cfg.WorktreeVar("alias"); v != "" {
				alias_var = v
			}
			if v := cfg.WorktreeVar("hostBuild"); v != "" {
				host_build_var = v
			}
			if v := cfg.EnvVar("lanDomain"); v != "" {
				lan_domain_var = v
			}
			if v := cfg.EnvVar("dbConnection"); v != "" {
				db_conn_var = v
			}
		}

		alias := read_env_file(full_path, env_filename, alias_var)
		if alias == "" {
			alias = name
		}

		container := read_container_name(full_path)
		if container == "" {
			// For shared compose: container name is project-service (e.g. bc-test-workflow-web)
			is_shared_compose := cfg != nil && cfg.Docker.ComposeStrategy != "generate" && cfg.ComposeFileAbs != ""
			if is_shared_compose {
				slug := read_env_file(full_path, env_filename, "BRANCH_SLUG")
				if slug == "" {
					slug = alias
				}
				primary := cfg.Services.Primary
				if primary == "" {
					for k := range cfg.Services.Ports {
						primary = k
						break
					}
				}
				container = fmt.Sprintf("%s-%s-%s", cfg.Name, slug, primary)
			} else if cfg != nil {
				container = cfg.ContainerName(name)
			} else {
				container = name
			}
		}

		mode := read_service_mode(full_path, cfg)
		offset := read_offset(full_path, cfg)
		host_build := read_env_file(full_path, env_filename, host_build_var) == "true"
		lan_domain := read_env_file(full_path, env_filename, lan_domain_var)

		var app_port int
		if cfg != nil {
			app_port = cfg.PrimaryPort() + offset
		} else {
			app_port = 3001 + offset
		}

		var container_prefix string
		if cfg != nil {
			container_prefix = cfg.ContainerPrefix()
		} else {
			container_prefix = ""
		}
		container_alias := strings.TrimPrefix(container, container_prefix)

		domain := lan_domain
		if domain == "" {
			if routes, ok := traefik_port_map[app_port]; ok {
				domain = resolve_traefik_domain(routes, container_alias)
			}
		}
		if domain == "" {
			if cfg != nil {
				domain = cfg.DomainFor(alias)
			} else {
				domain = fmt.Sprintf("%s.localhost", alias)
			}
		}

		mongo_url := read_env_file(full_path, env_filename, db_conn_var)
		var db_name string
		if mongo_url != "" {
			parts := strings.Split(mongo_url, "/")
			db_name = parts[len(parts)-1]
			if db_name == "" {
				if cfg != nil && cfg.Database != nil {
					db_name = cfg.Database.DefaultDb
				} else {
					db_name = "db"
				}
			}
		} else {
			if cfg != nil {
				db_name = cfg.DbName(alias)
			} else {
				safe_alias := regexp.MustCompile(`[^a-zA-Z0-9_]`).ReplaceAllString(alias, "_")
				db_name = fmt.Sprintf("db_%s", safe_alias)
			}
		}

		wt := Worktree{
			Path:      full_path,
			Name:      name,
			Type:      TypeDocker,
			Alias:     alias,
			Container: container,
			Mode:      mode,
			Branch:    branch,
			HostBuild: host_build,
			Domain:    domain,
			LANDomain: lan_domain,
			DBName:    db_name,
			Offset:    offset,
		}

		// Preserve runtime state from previous discovery
		if prev, ok := existing_map[full_path]; ok {
			wt.Running = prev.Running
			wt.ContainerExists = prev.ContainerExists
			wt.Health = prev.Health
			wt.Started = prev.Started
			wt.Uptime = prev.Uptime
			wt.CPU = prev.CPU
			wt.Mem = prev.Mem
			wt.MemPct = prev.MemPct
		}

		results = append(results, wt)
	}

	return results
}

// ResolveWorktreesDir computes the worktrees directory from a repo root.
// When cfg is non-nil, uses the config's resolved absolute path.
// e.g. /Users/x/apps/myapp -> /Users/x/apps/myapp-worktrees
func ResolveWorktreesDir(repo_root string, cfg *config.Config) string {
	if cfg != nil && cfg.WorktreesDirAbs != "" {
		return cfg.WorktreesDirAbs
	}
	// legacy fallback
	project_name := filepath.Base(repo_root)
	parent_dir := filepath.Dir(repo_root)
	return filepath.Join(parent_dir, fmt.Sprintf("%s-worktrees", project_name))
}

// SortWorktrees returns worktrees sorted:
// running docker > running local > stopped docker > stopped local > no-container
func SortWorktrees(worktrees []Worktree) []Worktree {
	var running_docker, running_local, stopped, local, no_container []Worktree

	for _, wt := range worktrees {
		switch {
		case wt.Type == TypeDocker && wt.Running:
			running_docker = append(running_docker, wt)
		case wt.Type == TypeLocal && wt.Running:
			running_local = append(running_local, wt)
		case wt.Type == TypeDocker && wt.ContainerExists:
			stopped = append(stopped, wt)
		case wt.Type == TypeLocal:
			local = append(local, wt)
		default:
			no_container = append(no_container, wt)
		}
	}

	var result []Worktree
	result = append(result, running_docker...)
	result = append(result, running_local...)
	result = append(result, stopped...)
	result = append(result, local...)
	result = append(result, no_container...)
	return result
}

func read_env_file(worktree_path, filename, key string) string {
	env_path := filepath.Join(worktree_path, filename)
	f, err := os.Open(env_path)
	if err != nil {
		return ""
	}
	defer f.Close()

	prefix := key + "="
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(line[len(prefix):])
		}
	}
	return ""
}

func read_container_name(worktree_path string) string {
	compose_path := filepath.Join(worktree_path, "docker-compose.worktree.yml")
	data, err := os.ReadFile(compose_path)
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`container_name:\s*(\S+)`)
	match := re.FindSubmatch(data)
	if match != nil {
		return string(match[1])
	}
	return ""
}

func read_service_mode(worktree_path string, cfg *config.Config) string {
	default_mode := "default"
	services_var := "WORKTREE_SERVICES"
	if cfg != nil {
		if cfg.Services.DefaultMode != "" {
			default_mode = cfg.Services.DefaultMode
		}
		if v := cfg.WorktreeVar("services"); v != "" {
			services_var = v
		}
	}

	compose_path := filepath.Join(worktree_path, "docker-compose.worktree.yml")
	data, err := os.ReadFile(compose_path)
	if err != nil {
		return default_mode
	}
	re := regexp.MustCompile(regexp.QuoteMeta(services_var) + `=(\w+)`)
	match := re.FindSubmatch(data)
	if match != nil {
		return string(match[1])
	}
	return default_mode
}

func read_offset(worktree_path string, cfg *config.Config) int {
	// Resolve env var names and port constants from config or use hardcoded defaults
	env_filename := ".env.worktree"
	host_offset_var := "WORKTREE_HOST_PORT_OFFSET"
	offset_var := "WORKTREE_PORT_OFFSET"
	base_var := "WORKTREE_PORT_BASE"
	port_base := 3000
	primary_port := 3001
	if cfg != nil {
		if cfg.Env.Filename != "" {
			env_filename = cfg.Env.Filename
		}
		if v := cfg.WorktreeVar("hostPortOffset"); v != "" {
			host_offset_var = v
		}
		if v := cfg.WorktreeVar("portOffset"); v != "" {
			offset_var = v
		}
		if v := cfg.WorktreeVar("portBase"); v != "" {
			base_var = v
		}
		if len(cfg.Services.Ports) > 0 {
			// Use the lowest port as the base
			first := true
			for _, p := range cfg.Services.Ports {
				if first || p < port_base {
					port_base = p
					first = false
				}
			}
		}
		if pp := cfg.PrimaryPort(); pp > 0 {
			primary_port = pp
		}
	}

	env_path := filepath.Join(worktree_path, env_filename)
	data, err := os.ReadFile(env_path)
	if err == nil {
		content := string(data)

		re_host := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(host_offset_var) + `=(\d+)`)
		if m := re_host.FindStringSubmatch(content); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil {
				return v
			}
		}

		re_offset := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(offset_var) + `=(\d+)`)
		if m := re_offset.FindStringSubmatch(content); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil {
				return v
			}
		}

		re_base := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(base_var) + `=(\d+)`)
		if m := re_base.FindStringSubmatch(content); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil {
				return v - port_base
			}
		}
	}

	compose_path := filepath.Join(worktree_path, "docker-compose.worktree.yml")
	cdata, err := os.ReadFile(compose_path)
	if err == nil {
		re_port := regexp.MustCompile(`"(\d+):` + strconv.Itoa(primary_port) + `"`)
		if m := re_port.FindStringSubmatch(string(cdata)); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil && v != primary_port {
				return v - primary_port
			}
		}
	}

	return 0
}

func read_branch(worktree_path string) string {
	git_path := filepath.Join(worktree_path, ".git")
	data, err := os.ReadFile(git_path)
	if err != nil {
		return ""
	}

	re := regexp.MustCompile(`gitdir:\s*(.+)`)
	match := re.FindSubmatch(data)
	if match == nil {
		return ""
	}

	gitdir := strings.TrimSpace(string(match[1]))
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(worktree_path, gitdir)
	}

	head_path := filepath.Join(gitdir, "HEAD")
	head_data, err := os.ReadFile(head_path)
	if err != nil {
		return ""
	}

	head_content := strings.TrimSpace(string(head_data))
	ref_re := regexp.MustCompile(`^ref: refs/heads/(.+)$`)
	if m := ref_re.FindStringSubmatch(head_content); m != nil {
		return m[1]
	}

	if len(head_content) >= 8 {
		return head_content[:8]
	}
	return head_content
}

func file_exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

type traefik_route struct {
	domain string
	name   string // filename without .yml
}

// build_traefik_port_map scans traefik dynamic configs and builds a map of
// app port -> list of routes pointing to that port.
func build_traefik_port_map(worktrees_dir string, cfg *config.Config) map[int][]traefik_route {
	var traefik_dir string
	if cfg != nil && cfg.ProxyDynamicDir != "" {
		traefik_dir = cfg.ProxyDynamicDir
	} else {
		traefik_dir = ""
	}

	if traefik_dir == "" {
		return nil
	}

	entries, err := os.ReadDir(traefik_dir)
	if err != nil {
		return nil
	}

	host_re := regexp.MustCompile(`Host\(` + "`" + `([^` + "`" + `]+)` + "`" + `\)`)
	port_re := regexp.MustCompile(`host\.docker\.internal:(\d+)`)

	result := make(map[int][]traefik_route)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(traefik_dir, entry.Name()))
		if err != nil {
			continue
		}
		content := string(data)

		host_match := host_re.FindStringSubmatch(content)
		if host_match == nil {
			continue
		}

		port_match := port_re.FindStringSubmatch(content)
		if port_match == nil {
			continue
		}
		port, err := strconv.Atoi(port_match[1])
		if err != nil {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".yml")
		result[port] = append(result[port], traefik_route{
			domain: host_match[1],
			name:   name,
		})
	}

	return result
}

// resolve_traefik_domain picks the best domain for a worktree from traefik routes.
// If only one route exists for the port, use it. If multiple exist, prefer the
// one whose name differs from the alias (the user-configured one over the auto-generated).
func resolve_traefik_domain(routes []traefik_route, alias string) string {
	if len(routes) == 0 {
		return ""
	}
	if len(routes) == 1 {
		return routes[0].domain
	}
	for _, r := range routes {
		if r.name != alias {
			return r.domain
		}
	}
	return routes[0].domain
}
