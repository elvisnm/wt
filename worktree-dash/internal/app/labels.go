package app

import "github.com/elvisnm/wt/internal/labels"

// Re-export label constants for backward compatibility within the app package.
// New code outside app should import labels directly.
const (
	LabelSep         = labels.Sep
	LabelCreate      = labels.Create
	LabelAWSKeys     = labels.AWSKeys
	LabelHeiHei      = labels.HeiHei
	LabelShell       = labels.Shell
	LabelClaude      = labels.Claude
	LabelZsh         = labels.Zsh
	LabelLogs        = labels.Logs
	LabelDev         = labels.Dev
	LabelBuild       = labels.Build
	LabelSkip        = labels.Skip
	LabelAdmin       = labels.Admin
	LabelLANOn       = labels.LANOn
	LabelLANOff      = labels.LANOff
	LabelDBSeed      = labels.DBSeed
	LabelDBDrop      = labels.DBDrop
	LabelDBReset     = labels.DBReset
	LabelFixImages   = labels.FixImages
	LabelDatabase    = labels.Database
	LabelHelp        = labels.Help
	LabelMaintenance = labels.Maintenance
	LabelPrune       = labels.Prune
	LabelAutostop    = labels.Autostop
	LabelRebuildBase = labels.RebuildBase
	LabelActions     = labels.Actions
	LabelRemove      = labels.Remove
)

// Picker context constants (app-internal state).
const (
	pickerWorktree    = "worktree"
	pickerDB          = "db"
	pickerMaintenance = "maintenance"
	pickerRemove      = "remove"
)

// tab_label formats a label with an alias suffix: "Prefix — alias".
func tab_label(prefix, alias string) string {
	return labels.Tab(prefix, alias)
}
