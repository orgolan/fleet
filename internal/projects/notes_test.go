package projects

import (
	"os"
	"strings"
	"testing"
)

func newProject(t *testing.T, name string) {
	t.Helper()
	t.Setenv("FLEET_PROJECTS", t.TempDir())
	if _, err := Add(name, newRepo(t), ""); err != nil {
		t.Fatal(err)
	}
}

func TestNotesAddNumberEditRemove(t *testing.T) {
	newProject(t, "site")
	nf, _ := LoadNotes("site")
	nf.Add(Conventions, "test with npm test")
	nf.Add(Log, "login form needs the CSRF token")
	nf.Add(Log, "footer links were broken")
	if err := nf.Save(); err != nil {
		t.Fatal(err)
	}

	nf, _ = LoadNotes("site")
	es := nf.Entries()
	if len(es) != 3 || es[0].Section != Conventions || es[0].Num != 1 || es[2].Num != 3 || !strings.Contains(es[1].Text, "CSRF") {
		t.Fatalf("entries = %+v", es)
	}
	if err := nf.Edit(2, "login form needs the CSRF token AND a session cookie"); err != nil {
		t.Fatal(err)
	}
	if got := nf.Entries()[1].Text; !datePrefix.MatchString(got) || !strings.Contains(got, "session cookie") {
		t.Fatalf("edit lost the date or text: %q", got)
	}
	if err := nf.Remove(1); err != nil {
		t.Fatal(err)
	}
	nf.Save()
	nf, _ = LoadNotes("site")
	if es := nf.Entries(); len(es) != 2 || es[0].Section != Log {
		t.Fatalf("after remove: %+v", es)
	}
	if err := nf.Remove(9); err == nil {
		t.Fatal("removing a missing note succeeded")
	}
}

// Old flat notes.md files (bullets, no sections) keep their entries as log entries.
func TestLegacyFlatNotesMigrateToLog(t *testing.T) {
	newProject(t, "old")
	dir, _ := entryDir("old")
	os.WriteFile(dir+"/notes.md", []byte("# old\n\nSome intro.\n- 2026-01-01: an old lesson\n"), 0o644)
	nf, err := LoadNotes("old")
	if err != nil {
		t.Fatal(err)
	}
	if es := nf.Entries(); len(es) != 1 || es[0].Section != Log || !strings.Contains(es[0].Text, "old lesson") {
		t.Fatalf("entries = %+v", es)
	}
	nf.Add(Conventions, "use make")
	nf.Save()
	b, _ := os.ReadFile(dir + "/notes.md")
	if !strings.Contains(string(b), "Some intro.") || !strings.Contains(string(b), "## Conventions\n- use make") {
		t.Fatalf("rewrite lost content:\n%s", b)
	}
}

func TestBriefSendsConventionsAndNewestLogOnly(t *testing.T) {
	newProject(t, "big")
	nf, _ := LoadNotes("big")
	nf.Add(Conventions, "always run tests")
	for i := 0; i < BriefLogEntries+3; i++ {
		nf.Add(Log, "entry number "+string(rune('a'+i)))
	}
	b := nf.Brief()
	if !strings.Contains(b, "always run tests") || !strings.Contains(b, "(3 older log entries omitted)") {
		t.Fatalf("brief = %q", b)
	}
	if strings.Contains(b, "entry number a") || !strings.Contains(b, "entry number m") {
		t.Fatalf("wrong log entries sent:\n%s", b)
	}
	empty := newEmpty(t)
	if empty != "" {
		t.Fatalf("empty notes gave a brief: %q", empty)
	}
}

func newEmpty(t *testing.T) string {
	newProject(t, "none")
	nf, _ := LoadNotes("none")
	return nf.Brief()
}
