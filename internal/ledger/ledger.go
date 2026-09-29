// Package ledger keeps one JSON file per task under the fleet state directory,
// so a later session can reconcile tasks against live herdr state.
package ledger

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Task is fleet's record of one spawned crewmate.
type Task struct {
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	Repo        string    `json:"repo"`
	Branch      string    `json:"branch"`
	Worktree    string    `json:"worktree"`
	WorkspaceID string    `json:"workspace_id"`
	PaneID      string    `json:"pane_id"`
	Brief       string    `json:"brief"`
	CreatedAt   time.Time `json:"created_at"`
}

// Dir is $FLEET_HOME, else ~/.local/state/fleet.
func Dir() (string, error) {
	if d := os.Getenv("FLEET_HOME"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "fleet"), nil
}

func path(name string) (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "tasks", name+".json"), nil
}

// Exists reports whether a task with this name is recorded.
func Exists(name string) (bool, error) {
	p, err := path(name)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// Save writes the task record.
func Save(t Task) error {
	p, err := path(t.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(b, '\n'), 0o644)
}

// List returns all recorded tasks.
func List() ([]Task, error) {
	d, err := Dir()
	if err != nil {
		return nil, err
	}
	files, err := filepath.Glob(filepath.Join(d, "tasks", "*.json"))
	if err != nil {
		return nil, err
	}
	var out []Task
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		var t Task
		if err := json.Unmarshal(b, &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}
