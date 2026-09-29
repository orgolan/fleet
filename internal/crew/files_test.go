package crew

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/orgolan/fleet/internal/ledger"
)

// gitRepo makes a repo with one commit on main.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "base"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestLongBriefGoesByFileAndStaysOutOfGit(t *testing.T) {
	srv, c := env(t)
	wt := gitRepo(t)
	brief := strings.Repeat("Do the whole thing carefully. ", 30)
	save(t, ledger.Task{Name: "a", PaneID: "w:p1", WorkspaceID: "wa", Worktree: wt, Brief: brief})
	srv.SetAgent("a", "w:p1", "idle", "")
	if err := Deliver(c, "a", false, false); err != nil {
		t.Fatal(err)
	}
	prompts, _ := srv.Snapshot()
	if len(prompts) != 1 || prompts[0] != "a: "+filePrompt {
		t.Fatalf("prompts = %q", prompts)
	}
	b, err := os.ReadFile(wt + "/.fleet/brief.md")
	if err != nil || !strings.Contains(string(b), brief) || !strings.Contains(string(b), ".fleet/report.md") {
		t.Fatalf("brief file = %q, %v", b, err)
	}
	if out, _ := exec.Command("git", "-C", wt, "status", "--porcelain").Output(); len(out) != 0 {
		t.Fatalf(".fleet leaks into git status: %q", out)
	}
}

func TestShortBriefStaysInline(t *testing.T) {
	srv, c := env(t)
	save(t, ledger.Task{Name: "a", PaneID: "w:p1", WorkspaceID: "wa", Worktree: t.TempDir(), Brief: "fix the typo"})
	srv.SetAgent("a", "w:p1", "idle", "")
	if err := Deliver(c, "a", false, false); err != nil {
		t.Fatal(err)
	}
	if p, _ := srv.Snapshot(); len(p) != 1 || p[0] != "a: fix the typo" {
		t.Fatalf("prompts = %q", p)
	}
}

func TestResultPrefersReportFileAndStopKeepsIt(t *testing.T) {
	srv, c := env(t)
	wt := t.TempDir()
	os.MkdirAll(wt+"/.fleet", 0o755)
	os.WriteFile(wt+"/.fleet/report.md", []byte("REPORT: all done\n"), 0o644)
	save(t, ledger.Task{Name: "a", PaneID: "w:p1", WorkspaceID: "wa", Worktree: wt, State: "idle"})
	srv.SetAgent("a", "w:p1", "idle", "terminal noise")
	srv.SetWorkspace("w:p1", "wa")
	txt, src, err := Result(c, "a", 20)
	if err != nil || !strings.Contains(txt, "REPORT: all done") || !strings.Contains(src, "report file") {
		t.Fatalf("Result = %q, %q, %v", txt, src, err)
	}
	if err := Stop(c, "a", false); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(wt) // the worktree is gone after a real stop
	srv.RemoveAgent("w:p1")
	txt, _, err = Result(c, "a", 20)
	if err != nil || !strings.Contains(txt, "REPORT: all done") {
		t.Fatalf("saved Result = %q, %v", txt, err)
	}
}

func TestGitInfo(t *testing.T) {
	wt := gitRepo(t)
	os.WriteFile(wt+"/new.txt", []byte("x"), 0o644)
	g := Git(ledger.Task{Worktree: wt, Base: "main"})
	if !g.OK || g.Dirty != 1 || g.Ahead != 0 || g.HasFile {
		t.Fatalf("Git = %+v", g)
	}
	if got := g.Short(); got != "+0 ~1" {
		t.Fatalf("Short = %q", got)
	}
	if long := g.Long("main"); !strings.Contains(long, "1 UNCOMMITTED") || !strings.Contains(long, "no .fleet/report.md") {
		t.Fatalf("Long = %q", long)
	}
	if g := Git(ledger.Task{Worktree: "/no/such/dir"}); g.OK || g.Short() != "-" {
		t.Fatalf("missing worktree: %+v", g)
	}
}
