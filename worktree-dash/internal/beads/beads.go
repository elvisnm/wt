package beads

import (
	"encoding/json"
	"os/exec"
	"time"
)

// Task represents a beads issue/task.
type Task struct {
	ID              string       `json:"id"`
	Title           string       `json:"title"`
	Description     string       `json:"description"`
	Status          string       `json:"status"`
	IssueType       string       `json:"issue_type"`
	Owner           string       `json:"owner"`
	Priority        int          `json:"priority"`
	Labels          []string     `json:"labels"`
	Dependencies    []Dependency `json:"dependencies"`
	DependencyCount int          `json:"dependency_count"`
	DependentCount  int          `json:"dependent_count"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       time.Time    `json:"updated_at"`
}

// Dependency represents a task dependency relationship.
type Dependency struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	DependencyType string `json:"dependency_type"`
}

// FetchTasks runs `bd list --json --status=open` and returns the parsed tasks.
func FetchTasks() ([]Task, error) {
	out, err := exec.Command("bd", "list", "--json", "--status=open").Output()
	if err != nil {
		return nil, err
	}
	var tasks []Task
	if err := json.Unmarshal(out, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

// CloseTask runs `bd close <id>`.
func CloseTask(id string) error {
	return exec.Command("bd", "close", id).Run()
}

// DeleteTask runs `bd update <id> --status=deleted`.
func DeleteTask(id string) error {
	return exec.Command("bd", "update", id, "--status=deleted").Run()
}

// FetchDetail runs `bd show --json <id>` and returns the expanded task.
func FetchDetail(id string) (*Task, error) {
	out, err := exec.Command("bd", "show", "--json", id).Output()
	if err != nil {
		return nil, err
	}
	// bd show --json wraps in an array
	var tasks []Task
	if err := json.Unmarshal(out, &tasks); err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, nil
	}
	return &tasks[0], nil
}
