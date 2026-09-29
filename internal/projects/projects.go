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

	"github.com/orgolan/fleet/internal/ledger"
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
	if _, err := entryDir(name); err != nil {
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
	return register(name, strings.TrimSpace(string(top)), base)
}

// New creates an empty git repo at projects/<name>/repo (with an initial commit,
// so worktrees can branch from it) and registers it.
func New(name string) (Project, error) {
	var zero Project
	dir, err := entryDir(name)
	if err != nil {
		return zero, err
	}
	if _, err := os.Stat(dir); err == nil {
		return zero, fmt.Errorf("project %q already exists", name)
	}
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		return zero, err
	}
	fail := func(err error) (Project, error) { os.RemoveAll(dir); return zero, err }
	if out, err := exec.Command("git", "-C", repo, "init", "-q", "-b", "main").CombinedOutput(); err != nil {
		return fail(fmt.Errorf("git init: %v: %s", err, strings.TrimSpace(string(out))))
	}
	commit := func(extra ...string) ([]byte, error) {
		args := append(append([]string{"-C", repo}, extra...), "commit", "-q", "--allow-empty", "-m", "Initial commit")
		return exec.Command("git", args...).CombinedOutput()
	}
	if _, err := commit(); err != nil {
		// No git identity configured: fall back so onboarding never dead-ends.
		if out, err := commit("-c", "user.name=fleet", "-c", "user.email=fleet@localhost"); err != nil {
			return fail(fmt.Errorf("initial commit: %v: %s", err, strings.TrimSpace(string(out))))
		}
	}
	p, err := register(name, repo, "main")
	if err != nil {
		return fail(err)
	}
	return p, nil
}

// Clone clones url to projects/<name>/repo and registers it.
func Clone(name, url string) (Project, error) {
	var zero Project
	dir, err := entryDir(name)
	if err != nil {
		return zero, err
	}
	if _, err := os.Stat(dir); err == nil {
		return zero, fmt.Errorf("project %q already exists", name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return zero, err
	}
	repo := filepath.Join(dir, "repo")
	if out, err := exec.Command("git", "clone", "-q", "--", url, repo).CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		return zero, fmt.Errorf("git clone: %v: %s", err, strings.TrimSpace(string(out)))
	}
	p, err := register(name, repo, "")
	if err != nil {
		os.RemoveAll(dir)
	}
	return p, err
}

// register writes project.json and a starter notes.md for a repo at path.
func register(name, path, base string) (Project, error) {
	var zero Project
	dir, _ := entryDir(name)
	if _, err := os.Stat(filepath.Join(dir, "project.json")); err == nil {
		return zero, fmt.Errorf("project %q already exists", name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return zero, err
	}
	p := Project{Name: name, Path: path, Base: base, AddedAt: time.Now().UTC()}
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

// Remove unregisters a project and deletes its notes. A repo registered with
// Add is never touched. A repo that lives inside the entry (created by New or
// Clone) holds real work, so it is only deleted when force is set.
func Remove(name string, force bool) error {
	p, err := Get(name)
	if err != nil {
		return err
	}
	dir, _ := entryDir(name)
	if rel, err := filepath.Rel(dir, p.Path); err == nil && !strings.HasPrefix(rel, "..") && !force {
		return fmt.Errorf("project %q owns its repo at %s; removing it would delete that work: move the repo out first, or use --force", name, p.Path)
	}
	return os.RemoveAll(dir)
}
