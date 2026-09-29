package projects

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func newRepo(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if out, err := exec.Command("git", "-C", d, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return d
}

func TestAddListNotesRemove(t *testing.T) {
	t.Setenv("FLEET_PROJECTS", t.TempDir())
	repo := newRepo(t)
	p, err := Add("site", repo, "main")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Get("site"); err != nil || got.Path != p.Path || got.Base != "main" {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	if _, err := Add("site", repo, ""); err == nil {
		t.Fatal("duplicate add succeeded")
	}
	if err := AddNote("site", "run npm test"); err != nil {
		t.Fatal(err)
	}
	if n, _ := Notes("site"); !strings.Contains(n, "run npm test") {
		t.Fatalf("notes = %q", n)
	}
	if ps, _ := List(); len(ps) != 1 || ps[0].Name != "site" {
		t.Fatalf("List = %+v", ps)
	}
	if err := Remove("site"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(repo); err != nil {
		t.Fatal("Remove touched the repo")
	}
	if _, err := Get("site"); err == nil {
		t.Fatal("project still present")
	}
}

func TestRejectsNonRepoAndBadName(t *testing.T) {
	t.Setenv("FLEET_PROJECTS", t.TempDir())
	if _, err := Add("x", t.TempDir(), ""); err == nil {
		t.Fatal("non-repo accepted")
	}
	if _, err := Add("Bad Name", newRepo(t), ""); err == nil {
		t.Fatal("bad name accepted")
	}
}

// The tracked example folder must never show up as a real project.
func TestListIgnoresExample(t *testing.T) {
	d := t.TempDir()
	t.Setenv("FLEET_PROJECTS", d)
	os.MkdirAll(d+"/_example", 0o755)
	if ps, err := List(); err != nil || len(ps) != 0 {
		t.Fatalf("List = %+v, %v", ps, err)
	}
}
