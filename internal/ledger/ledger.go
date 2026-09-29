// Package ledger keeps one JSON file per task under the fleet state directory,
// so a later session can reconcile tasks against live herdr state.
package ledger

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
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
	BriefSent   bool      `json:"brief_sent"`
	State       string    `json:"state,omitempty"` // last observed: working, idle, blocked, done, exited
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
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
	t.UpdatedAt = time.Now().UTC()
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Load reads one task.
func Load(name string) (Task, error) {
	var t Task
	p, err := path(name)
	if err != nil {
		return t, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return t, err
	}
	return t, json.Unmarshal(b, &t)
}

// ErrLocked is returned by WithLock when block is false and another process holds the lock.
var ErrLocked = errors.New("ledger: task is locked")

// WithLock runs fn holding an exclusive per-task action lock, so the spawner
// and the supervisor never act on the same task (e.g. send its brief) at once.
// It can be held for a long time; record changes go through Update instead.
func WithLock(name string, block bool, fn func() error) error {
	return flock(name, ".lock", block, fn)
}

// Update applies fn to the stored task under a short read-modify-write lock and
// saves the result if it changed. It never waits on WithLock, so a long brief
// delivery cannot make state updates get lost, and concurrent updaters cannot
// overwrite each other's fields.
func Update(name string, fn func(*Task)) error {
	return flock(name, ".rw", true, func() error {
		t, err := Load(name)
		if err != nil {
			return err
		}
		before := t
		fn(&t)
		if t == before {
			return nil
		}
		return Save(t)
	})
}

func flock(name, suffix string, block bool, fn func() error) error {
	p, err := path(name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p+suffix, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	how := syscall.LOCK_EX
	if !block {
		how |= syscall.LOCK_NB
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return ErrLocked
		}
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
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
