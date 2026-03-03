package app

import "fmt"

// Terminal session label prefixes.
// The em-dash separator " — " is used between prefix and alias.
const (
	LabelSep = " — "

	LabelCreate    = "Create"
	LabelAWSKeys   = "AWS Keys"
	LabelHeiHei    = "HeiHei"
	LabelShell     = "Shell"
	LabelClaude    = "Claude"
	LabelZsh       = "Zsh"
	LabelLogs      = "Logs"
	LabelDev       = "Dev"
	LabelBuild     = "Build"
	LabelSkip      = "Skip"
	LabelAdmin     = "Admin"
	LabelLANOn     = "LAN On"
	LabelLANOff    = "LAN Off"
	LabelDBSeed    = "DB Seed"
	LabelDBDrop    = "DB Drop"
	LabelDBReset   = "DB Reset"
	LabelFixImages = "Fix Images"
	LabelDatabase  = "Database"
)

// tab_label formats a label with an alias suffix: "Prefix — alias".
func tab_label(prefix, alias string) string {
	return fmt.Sprintf("%s%s%s", prefix, LabelSep, alias)
}
