// Package projects is fleet's registry of the repos a captain has put in scope,
// with free-form notes per project that the first mate reads before briefing a
// crewmate. Entries live in a projects/ folder: projects/<name>/project.json
// and projects/<name>/notes.md.
package projects

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"fleet/internal/ledger"
)

// Names follow the same rule as crewmate names, which also keeps the tracked
// projects/_example folder out of listings.
var nameRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// Project is one registered repo.
type Project struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Base    string    `json:"base,omitempty"` // default base ref for new branches
	AddedAt time.Time `json:"added_at"`
}

// Dir is $FLEET_PROJECTS, else projects/ next to the fleet checkout the binary
// was built in (bin/fleet), else $FLEET_HOME/projects.
func Dir() (string, error) {
	if d := os.Getenv("FLEET_PROJECTS"); d != "" {
		return d, nil
	}
	if exe, err := os.Executable(); err == nil {
		if exe, err = filepath.EvalSymlinks(exe); err == nil {
			root := filepath.Dir(filepath.Dir(exe))
			if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil && filepath.Base(filepath.Dir(exe)) == "bin" {
				return filepath.Join(root, "projects"), nil
			}
		}
	}
	h, err := ledger.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "projects"), nil
}

func entryDir(name string) (string, error) {
	if !nameRE.MatchString(name) {
		return "", fmt.Errorf("invalid project name %q: must match %s", name, nameRE)
	}
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, name), nil
}

// Add registers the git repo containing path under name.
func Add(name, path, base string) (Project, error) {
	var zero Project
	dir, err := entryDir(name)
	if err != nil {
		return zero, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return zero, err
	}
	top, err := exec.Command("git", "-C", abs, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return zero, fmt.Errorf("%s is not inside a git repo", abs)
	}
	p := Project{Name: name, Path: strings.TrimSpace(string(top)), Base: base, AddedAt: time.Now().UTC()}
	if _, err := os.Stat(dir); err == nil {
		return zero, fmt.Errorf("project %q already exists", name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return zero, err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return zero, err
	}
	if err := os.WriteFile(filepath.Join(dir, "project.json"), append(b, '\n'), 0o644); err != nil {
		return zero, err
	}
	notes := fmt.Sprintf("# %s\n\nNotes for the first mate: conventions, build/test commands, gotchas, past outcomes.\n", name)
	return p, os.WriteFile(filepath.Join(dir, "notes.md"), []byte(notes), 0o644)
}

// Get loads one project.
func Get(name string) (Project, error) {
	var p Project
	dir, err := entryDir(name)
	if err != nil {
		return p, err
	}
	b, err := os.ReadFile(filepath.Join(dir, "project.json"))
	if errors.Is(err, os.ErrNotExist) {
		return p, fmt.Errorf("unknown project %q (see `fleet project list`)", name)
	}
	if err != nil {
		return p, err
	}
	return p, json.Unmarshal(b, &p)
}

// List returns all registered projects, sorted by name.
func List() ([]Project, error) {
	d, err := Dir()
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(d)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Project
	for _, e := range ents {
		if !e.IsDir() || !nameRE.MatchString(e.Name()) {
			continue
		}
		p, err := Get(e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Notes returns the project's notes.md ("" if none).
func Notes(name string) (string, error) {
	dir, err := entryDir(name)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(dir, "notes.md"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

// AddNote appends a dated bullet to the project's notes.
func AddNote(name, text string) error {
	if _, err := Get(name); err != nil {
		return err
	}
	dir, _ := entryDir(name)
	f, err := os.OpenFile(filepath.Join(dir, "notes.md"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "- %s: %s\n", time.Now().Format("2006-01-02"), strings.TrimSpace(text))
	return err
}

// Remove unregisters a project and deletes its notes. The repo is untouched.
func Remove(name string) error {
	if _, err := Get(name); err != nil {
		return err
	}
	dir, _ := entryDir(name)
	return os.RemoveAll(dir)
}
