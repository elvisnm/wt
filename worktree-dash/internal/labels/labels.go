package labels

import "fmt"

// Terminal session label constants.
// Used by app, ui, and terminal packages.
const (
	Sep = " — "

	Create    = "Create"
	AWSKeys   = "AWS Keys"
	HeiHei    = "HeiHei"
	Shell     = "Shell"
	Claude    = "Claude"
	Zsh       = "Zsh"
	Logs      = "Logs"
	Dev       = "Dev"
	Build     = "Build"
	Skip      = "Skip"
	Admin     = "Admin"
	LANOn     = "LAN On"
	LANOff    = "LAN Off"
	DBSeed    = "DB Seed"
	DBDrop    = "DB Drop"
	DBReset   = "DB Reset"
	FixImages   = "Fix Images"
	Database    = "Database"
	Help        = "Help"
	Maintenance = "Maintenance"
	Prune       = "Prune Volumes"
	Autostop    = "Autostop Idle"
	RebuildBase = "Rebuild Base"
	Actions     = "Actions"
	Remove      = "Remove"
)

// Tab formats a label with an alias suffix: "Prefix — alias".
func Tab(prefix, alias string) string {
	return fmt.Sprintf("%s%s%s", prefix, Sep, alias)
}
