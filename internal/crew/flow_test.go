package crew

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/orgolan/fleet/internal/ledger"
)

func gitc(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// mergeRepo is a repo on main plus a linked worktree on fleet/x holding one commit
// (b.txt) of its own, and a task recording both.
func mergeRepo(t *testing.T) (repo, wt string, task ledger.Task) {
	t.Helper()
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(k, "t")
	}
	for _, k := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "t@t")
	}
	repo = gitRepo(t)
	os.WriteFile(repo+"/a.txt", []byte("a\n"), 0o644)
	gitc(t, repo, "add", ".")
	gitc(t, repo, "commit", "-q", "-m", "a")
	base := gitc(t, repo, "rev-parse", "HEAD")
	wt = t.TempDir() + "/wt"
	gitc(t, repo, "worktree", "add", "-q", "-b", "fleet/x", wt)
	os.WriteFile(wt+"/b.txt", []byte("b\n"), 0o644)
	gitc(t, wt, "add", ".")
	gitc(t, wt, "commit", "-q", "-m", "crew work")
	task = ledger.Task{Name: "x", Kind: "claude", Repo: repo, Branch: "fleet/x", Base: "main", BaseRev: base,
		Worktree: wt, WorkspaceID: "wx", PaneID: "w:p1", State: "idle"}
	return repo, wt, task
}

func TestMergeLandsBranchAndStopsCrewmate(t *testing.T) {
	srv, c := env(t)
	repo, _, task := mergeRepo(t)
	save(t, task)
	srv.SetAgent("x", "w:p1", "idle", "")
	res, err := Merge(c, "x", MergeOpts{Test: "test -f b.txt"})
	if err != nil || res.AlreadyMerged || !res.Stopped {
		t.Fatalf("Merge = %+v, %v", res, err)
	}
	if _, err := os.Stat(repo + "/b.txt"); err != nil {
		t.Fatal("b.txt not on main")
	}
	if subj := gitc(t, repo, "log", "-1", "--format=%s"); !strings.HasPrefix(subj, "Merge x") {
		t.Fatalf("subject = %q", subj)
	}
	if got, _ := ledger.Load("x"); got.State != "stopped" {
		t.Fatalf("state = %q", got.State)
	}
}

func TestMergeConflictAbortsCleanly(t *testing.T) {
	srv, c := env(t)
	repo, wt, task := mergeRepo(t)
	os.WriteFile(wt+"/a.txt", []byte("crew\n"), 0o644)
	gitc(t, wt, "commit", "-q", "-am", "crew edits a")
	os.WriteFile(repo+"/a.txt", []byte("main\n"), 0o644)
	gitc(t, repo, "commit", "-q", "-am", "main edits a")
	save(t, task)
	srv.SetAgent("x", "w:p1", "idle", "")
	_, err := Merge(c, "x", MergeOpts{})
	if err == nil || !strings.Contains(err.Error(), "conflict in: a.txt") {
		t.Fatalf("err = %v", err)
	}
	if out, _ := exec.Command("git", "-C", repo, "rev-parse", "-q", "--verify", "MERGE_HEAD").Output(); len(out) != 0 {
		t.Fatal("merge left in progress")
	}
	if b, _ := os.ReadFile(repo + "/a.txt"); string(b) != "main\n" {
		t.Fatalf("base changed: %q", b)
	}
	if got, _ := ledger.Load("x"); got.State == "stopped" {
		t.Fatal("crewmate stopped despite the failed merge")
	}
}

func TestMergeTestFailureAbortsAndKeepsBaseUntouched(t *testing.T) {
	srv, c := env(t)
	repo, _, task := mergeRepo(t)
	save(t, task)
	srv.SetAgent("x", "w:p1", "idle", "")
	_, err := Merge(c, "x", MergeOpts{Test: "echo boom; exit 3"})
	if err == nil || !strings.Contains(err.Error(), "test command failed") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(repo + "/b.txt"); err == nil {
		t.Fatal("b.txt landed despite the failing test")
	}
	if out, _ := exec.Command("git", "-C", repo, "rev-parse", "-q", "--verify", "MERGE_HEAD").Output(); len(out) != 0 {
		t.Fatal("merge left in progress")
	}
}

func TestMergeRefusesWorkingDirtyOrOffBase(t *testing.T) {
	srv, c := env(t)
	repo, wt, task := mergeRepo(t)
	save(t, task)
	srv.SetAgent("x", "w:p1", "working", "")
	if _, err := Merge(c, "x", MergeOpts{}); err == nil || !strings.Contains(err.Error(), "still working") {
		t.Fatalf("working: %v", err)
	}
	srv.SetAgent("x", "w:p1", "idle", "")
	os.WriteFile(wt+"/wip.txt", []byte("w"), 0o644)
	if _, err := Merge(c, "x", MergeOpts{}); err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Fatalf("dirty worktree: %v", err)
	}
	os.Remove(wt + "/wip.txt")
	gitc(t, repo, "checkout", "-q", "-b", "other")
	if _, err := Merge(c, "x", MergeOpts{}); err == nil || !strings.Contains(err.Error(), "not \"main\"") {
		t.Fatalf("off base: %v", err)
	}
}

func TestMergeWarnsAboutLargeFiles(t *testing.T) {
	srv, c := env(t)
	_, wt, task := mergeRepo(t)
	os.WriteFile(wt+"/big.bin", make([]byte, 6<<20), 0o644)
	gitc(t, wt, "add", ".")
	gitc(t, wt, "commit", "-q", "-m", "big")
	save(t, task)
	srv.SetAgent("x", "w:p1", "idle", "")
	res, err := Merge(c, "x", MergeOpts{Keep: true})
	if err != nil || len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "big.bin is 6.0 MB") {
		t.Fatalf("Merge = %+v, %v", res, err)
	}
}

func TestWaitReturnsWhenACrewmateSettlesAndTimesOut(t *testing.T) {
	srv, c := env(t)
	save(t, ledger.Task{Name: "a", Kind: "claude", PaneID: "w:p1", WorkspaceID: "wa", Brief: "x", BriefSent: true, State: "working"})
	srv.SetAgent("a", "w:p1", "working", "")
	go func() { time.Sleep(150 * time.Millisecond); srv.SetAgent("a", "w:p1", "idle", "") }()
	rows, err := Wait(c, []string{"a"}, false, 5*time.Second, 20*time.Millisecond)
	if err != nil || len(rows) != 1 || rows[0].Live != "idle" {
		t.Fatalf("Wait = %+v, %v", rows, err)
	}
	srv.SetAgent("a", "w:p1", "working", "")
	if _, err := Wait(c, nil, true, 100*time.Millisecond, 20*time.Millisecond); !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v", err)
	}
	if _, err := Wait(c, []string{"nope"}, false, time.Second, 20*time.Millisecond); err == nil {
		t.Fatal("unknown task accepted")
	}
}

func TestResumeRelaunchesAnExitedCrewmate(t *testing.T) {
	srv, c := env(t)
	save(t, ledger.Task{Name: "a", Kind: "claude", PaneID: "w:old", WorkspaceID: "wold", Worktree: t.TempDir(), Brief: "x", BriefSent: true, State: "exited"})
	if err := Resume(c, "a"); err != nil {
		t.Fatal(err)
	}
	if st := srv.StartedAgents(); len(st) != 1 || st[0] != "a claude --continue" {
		t.Fatalf("started = %v", st)
	}
	got, _ := ledger.Load("a")
	if got.PaneID != "w:new" || got.State != "working" {
		t.Fatalf("task = %+v", got)
	}
	if p, _ := srv.Snapshot(); len(p) != 1 || !strings.HasPrefix(p[0], "a: Your previous session ended") {
		t.Fatalf("prompts = %v", p)
	}
	if cl := srv.ClosedWorkspaces(); len(cl) != 1 || cl[0] != "wold" {
		t.Fatalf("closed = %v", cl)
	}
	// Running agents are not resumed.
	srv.SetAgent("a", "w:new", "idle", "")
	if err := Resume(c, "a"); err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("second resume: %v", err)
	}
}

// A resumed crewmate's workspace is not a herdr-managed worktree checkout, so
// herdr refuses to remove it; stop then closes the workspace and git removes the
// worktree, under the same safety rules.
func TestStopUnmanagedWorkspaceRemovesWorktreeWithGit(t *testing.T) {
	srv, c := env(t)
	repo, wt, task := mergeRepo(t)
	gitc(t, repo, "merge", "-q", "--no-ff", "-m", "Merge x", "fleet/x") // its work is merged
	save(t, task)
	srv.SetAgent("x", "w:p1", "idle", "")
	srv.SetWorkspace("w:p1", "wx")
	srv.SetUnmanaged("wx")
	if err := Stop(c, "x", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt); err == nil {
		t.Fatal("worktree still on disk")
	}
	if cl := srv.ClosedWorkspaces(); len(cl) != 1 || cl[0] != "wx" {
		t.Fatalf("closed = %v", cl)
	}
	if got, _ := ledger.Load("x"); got.State != "stopped" {
		t.Fatalf("state = %q", got.State)
	}
}

func TestStopUnmanagedKeepsUnmergedOrDirtyWorkUnlessForced(t *testing.T) {
	srv, c := env(t)
	_, wt, task := mergeRepo(t) // fleet/x has a commit main lacks
	save(t, task)
	srv.SetAgent("x", "w:p1", "idle", "")
	srv.SetUnmanaged("wx")
	err := Stop(c, "x", false)
	if err == nil || !strings.Contains(err.Error(), "not merged") {
		t.Fatalf("unmerged: %v", err)
	}
	if got, _ := ledger.Load("x"); got.State != "idle" {
		t.Fatalf("state not restored: %q", got.State)
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatal("worktree removed despite the refusal")
	}
	if err := Stop(c, "x", true); err != nil {
		t.Fatalf("forced: %v", err)
	}
	if _, err := os.Stat(wt); err == nil {
		t.Fatal("forced stop left the worktree")
	}
}
