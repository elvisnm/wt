package worktree

// WorktreeType distinguishes docker-based from local worktrees
type WorktreeType string

const (
	TypeDocker WorktreeType = "docker"
	TypeLocal  WorktreeType = "local"
)

// Worktree represents a single git worktree with optional Docker container
type Worktree struct {
	Path      string
	Name      string
	Type      WorktreeType
	Alias     string
	Container string
	Mode      string // "full" or "minimal"
	Branch    string

	// Docker-specific
	HostBuild bool
	Domain    string
	LANDomain string
	DBName    string
	Offset    int
	Ports     map[string]int

	// Runtime state
	Running         bool
	ContainerExists bool
	Health          string // "healthy", "starting", or ""
	Started         string
	Uptime          string
	CPU             string
	Mem             string
	MemPct          string
}

// Service represents a PM2 service inside a container
type Service struct {
	Name         string
	DisplayName  string
	Status       string // "online", "stopped", "unknown"
	Memory       int64
	CPU          float64
	RestartCount int
}
