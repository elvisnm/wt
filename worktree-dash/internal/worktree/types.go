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
	Mode      string // service mode from config (e.g. "default", "full", "minimal")
	Branch    string

	// Docker-specific
	HostBuild bool
	Domain    string
	LANDomain string
	DBName    string
	Offset    int
	Ports     map[string]int

	// Local worktree isolation
	IsolatedPM2 bool // true when worktree has its own .pm2 directory

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

// PM2Home returns the path to the isolated PM2 home directory.
// Only meaningful when IsolatedPM2 is true.
func (wt *Worktree) PM2Home() string {
	return wt.Path + "/.pm2"
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
