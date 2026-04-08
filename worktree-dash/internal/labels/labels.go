package labels

// Terminal session label constants.
// Used by app, ui, and terminal packages.
const (
	Sep = " — "

	Create    = "Create"
	HeiHei    = "HeiHei"
	Shell     = "Shell"
	Claude    = "Claude"
	Zsh       = "Zsh"
	Logs      = "Logs"
	Dev       = "Dev"
	Skip      = "Skip"
	LANOn     = "LAN On"
	LANOff    = "LAN Off"
	Database    = "Database"
	Help        = "Help"
	Settings    = "Settings"
	Maintenance = "Maintenance"
	Prune       = "Prune Volumes"
	Autostop    = "Autostop Idle"
	RebuildBase = "Rebuild Base"
	Actions     = "Actions"
	Pull        = "Pull"
	Remove      = "Remove"
)

// Tab formats a label with an alias suffix: "Prefix — alias".
func Tab(prefix, alias string) string {
	return prefix + Sep + alias
}
