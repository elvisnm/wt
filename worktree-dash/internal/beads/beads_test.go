package beads

import (
	"encoding/json"
	"testing"
)

func TestTaskUnmarshal(t *testing.T) {
	raw := `[
		{
			"id": "wt-abc",
			"title": "Fix the widget",
			"description": "The widget is broken",
			"status": "open",
			"priority": 1,
			"issue_type": "bug",
			"owner": "alice",
			"labels": ["urgent", "frontend"],
			"dependency_count": 2,
			"dependent_count": 0,
			"created_at": "2026-01-15T10:00:00Z",
			"updated_at": "2026-01-16T12:00:00Z"
		},
		{
			"id": "wt-def",
			"title": "Add feature Y",
			"status": "in_progress",
			"priority": 3,
			"issue_type": "feature",
			"dependency_count": 0,
			"dependent_count": 1,
			"created_at": "2026-01-10T08:00:00Z",
			"updated_at": "2026-01-10T08:00:00Z"
		}
	]`

	var tasks []Task
	if err := json.Unmarshal([]byte(raw), &tasks); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}

	// First task
	if tasks[0].ID != "wt-abc" {
		t.Errorf("expected id wt-abc, got %s", tasks[0].ID)
	}
	if tasks[0].Title != "Fix the widget" {
		t.Errorf("expected title 'Fix the widget', got %s", tasks[0].Title)
	}
	if tasks[0].Priority != 1 {
		t.Errorf("expected priority 1, got %d", tasks[0].Priority)
	}
	if tasks[0].IssueType != "bug" {
		t.Errorf("expected issue_type bug, got %s", tasks[0].IssueType)
	}
	if len(tasks[0].Labels) != 2 {
		t.Errorf("expected 2 labels, got %d", len(tasks[0].Labels))
	}
	if tasks[0].DependencyCount != 2 {
		t.Errorf("expected dependency_count 2, got %d", tasks[0].DependencyCount)
	}

	// Second task
	if tasks[1].ID != "wt-def" {
		t.Errorf("expected id wt-def, got %s", tasks[1].ID)
	}
	if tasks[1].Status != "in_progress" {
		t.Errorf("expected status in_progress, got %s", tasks[1].Status)
	}
}

func TestDetailUnmarshal(t *testing.T) {
	raw := `[
		{
			"id": "wt-abc",
			"title": "Fix the widget",
			"description": "Detailed description here",
			"status": "open",
			"priority": 1,
			"issue_type": "bug",
			"dependencies": [
				{
					"id": "wt-xyz",
					"title": "Setup environment",
					"dependency_type": "blocks"
				}
			],
			"dependency_count": 1,
			"dependent_count": 0,
			"created_at": "2026-01-15T10:00:00Z",
			"updated_at": "2026-01-16T12:00:00Z"
		}
	]`

	var tasks []Task
	if err := json.Unmarshal([]byte(raw), &tasks); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	task := tasks[0]
	if len(task.Dependencies) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(task.Dependencies))
	}
	if task.Dependencies[0].ID != "wt-xyz" {
		t.Errorf("expected dependency id wt-xyz, got %s", task.Dependencies[0].ID)
	}
	if task.Dependencies[0].DependencyType != "blocks" {
		t.Errorf("expected dependency_type blocks, got %s", task.Dependencies[0].DependencyType)
	}
}
