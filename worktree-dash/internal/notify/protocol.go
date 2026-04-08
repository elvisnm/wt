package notify

import "encoding/json"

// Command types sent over the FIFO as JSON lines.
const (
	CmdClear   = "clear"
	CmdNotify  = "notify"
	CmdPicker  = "picker"
	CmdConfirm = "confirm"
	CmdInput   = "input"
)

// Command is a render instruction received over the FIFO.
// Only fields relevant to the command type are populated.
type Command struct {
	Cmd      string   `json:"cmd"`
	Title    string   `json:"title,omitempty"`
	Message  string   `json:"message,omitempty"`
	Prompt   string   `json:"prompt,omitempty"`
	Options  []string `json:"options,omitempty"`
	Sentinel string   `json:"sentinel,omitempty"`
	Rows     int      `json:"rows,omitempty"` // optional height hint
}

// ParseCommand decodes a single JSON line into a Command.
func ParseCommand(data []byte) (*Command, error) {
	var cmd Command
	if err := json.Unmarshal(data, &cmd); err != nil {
		return nil, err
	}
	return &cmd, nil
}
