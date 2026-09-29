package supervisor

import (
	"context"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/orgolan/fleet/internal/crew"
	"github.com/orgolan/fleet/internal/herdr"
	"github.com/orgolan/fleet/internal/herdr/fake"
	"github.com/orgolan/fleet/internal/ledger"
)

func setup(t *testing.T, task ledger.Task) (*fake.Server, context.CancelFunc) {
	t.Helper()
	return setupWith(t, task, nil)
}

// setupWith is setup with a hook to tune the supervisor before it runs. Settle and
// Verify are off unless the hook turns them on.
func setupWith(t *testing.T, task ledger.Task, tune func(*Supervisor)) (*fake.Server, context.CancelFunc) {
	t.Helper()
	t.Setenv("FLEET_HOME", t.TempDir())
	if err := ledger.Save(task); err != nil {
		t.Fatal(err)
	}
	srv := fake.New(t)
	s := New(&herdr.Client{Socket: srv.Socket}, log.New(io.Discard, "", 0))
	s.Poll = time.Hour // events only: the reconcile tick must not mask event bugs
	s.Settle, s.Verify = 0, 0
	if tune != nil {
		tune(s)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go s.Run(ctx)
	return srv, cancel
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

func TestBlockedThenIdleDeliversBriefOnce(t *testing.T) {
	task := ledger.Task{Name: "a1", Kind: "claude", PaneID: "w:p1", Brief: "do the thing", CreatedAt: time.Now()}
	srv, _ := setup(t, task)
	srv.SetAgent("a1", "w:p1", "blocked", "Do you trust this folder?")

	// The initial reconcile sees the agent blocked before any brief is sent.
	eventually(t, "needs-you notification", func() bool {
		_, n := srv.Snapshot()
		return len(n) == 1 && strings.Contains(n[0], "needs you")
	})
	if p, _ := srv.Snapshot(); len(p) != 0 {
		t.Fatalf("brief sent to a blocked agent: %v", p)
	}

	// The captain resolves the prompt; the agent goes idle. Repeat the event to prove idempotence.
	srv.SetAgent("a1", "w:p1", "idle", "")
	for i := 0; i < 2; i++ {
		srv.Emit("pane.agent_status_changed", "w:p1", map[string]any{"workspace_id": "w", "agent_status": "idle"})
	}
	eventually(t, "brief delivered", func() bool { p, _ := srv.Snapshot(); return len(p) == 1 })
	time.Sleep(100 * time.Millisecond)
	if p, _ := srv.Snapshot(); len(p) != 1 || p[0] != "a1: do the thing" {
		t.Fatalf("prompts = %v", p)
	}
	got, err := ledger.Load("a1")
	if err != nil || !got.BriefSent {
		t.Fatalf("ledger not updated: %+v %v", got, err)
	}
}

func TestFinishedTurnNotifiesAndExitIsRecorded(t *testing.T) {
	task := ledger.Task{Name: "b1", Kind: "claude", PaneID: "w:p2", Brief: "x", BriefSent: true, CreatedAt: time.Now()}
	srv, _ := setup(t, task)
	srv.SetAgent("b1", "w:p2", "working", "")
	eventually(t, "state working", func() bool { got, _ := ledger.Load("b1"); return got.State == "working" })

	srv.SetAgent("b1", "w:p2", "done", "all finished")
	srv.Emit("pane.agent_status_changed", "w:p2", map[string]any{"workspace_id": "w", "agent_status": "done"})
	eventually(t, "finished notification", func() bool {
		_, n := srv.Snapshot()
		return len(n) == 1 && strings.Contains(n[0], "finished")
	})

	srv.Emit("pane.exited", "w:p2", map[string]any{"workspace_id": "w"})
	eventually(t, "exited recorded", func() bool { got, _ := ledger.Load("b1"); return got.State == "exited" })
	eventually(t, "exited notification", func() bool { _, n := srv.Snapshot(); return len(n) == 2 })
}

func TestStopDoesNotTriggerExitedToast(t *testing.T) {
	task := ledger.Task{Name: "s1", Kind: "claude", PaneID: "w:p3", WorkspaceID: "ws", Brief: "x", BriefSent: true, CreatedAt: time.Now()}
	srv, _ := setup(t, task)
	srv.SetAgent("s1", "w:p3", "working", "")
	srv.SetWorkspace("w:p3", "ws")
	eventually(t, "state working", func() bool { got, _ := ledger.Load("s1"); return got.State == "working" })

	// Stopping closes the pane (the fake emits pane.closed), which the
	// supervisor must not report as the agent exiting.
	if err := crew.Stop(&herdr.Client{Socket: srv.Socket}, "s1", false); err != nil {
		t.Fatal(err)
	}
	// Also feed a stale exit event, as a racing reconcile could.
	srv.Emit("pane.exited", "w:p3", map[string]any{"workspace_id": "ws"})
	time.Sleep(300 * time.Millisecond)
	if _, n := srv.Snapshot(); len(n) != 0 {
		t.Fatalf("spurious notifications: %v", n)
	}
	if got, _ := ledger.Load("s1"); got.State != "stopped" {
		t.Fatalf("state = %q, want stopped", got.State)
	}
}

// exited() receives a task copy read before `fleet stop` marked it stopped
// (taskByPane/reconcile race). The ledger re-read guard must swallow it: no
// toast, state stays stopped. The event path filters ended tasks up front, so
// the guard is only reachable with a stale copy, hence the direct call.
func TestExitedWithStaleCopyOfStoppedTaskIsSilent(t *testing.T) {
	task := ledger.Task{Name: "g1", Kind: "claude", PaneID: "w:p9", WorkspaceID: "ws", Brief: "x", BriefSent: true, State: "working", CreatedAt: time.Now()}
	t.Setenv("FLEET_HOME", t.TempDir())
	if err := ledger.Save(task); err != nil {
		t.Fatal(err)
	}
	srv := fake.New(t)
	s := New(&herdr.Client{Socket: srv.Socket}, log.New(io.Discard, "", 0))

	stale := task // what the supervisor read before the stop
	if err := ledger.Update("g1", func(x *ledger.Task) { x.State = "stopped" }); err != nil {
		t.Fatal(err)
	}
	s.exited(stale)

	if _, n := srv.Snapshot(); len(n) != 0 {
		t.Fatalf("spurious notifications: %v", n)
	}
	if got, _ := ledger.Load("g1"); got.State != "stopped" {
		t.Fatalf("state = %q, want stopped", got.State)
	}
}

// A crewmate whose agent appears after the supervisor started must be picked
// up from the agent_detected event alone (Poll is an hour), and its later
// status changes must arrive through the per-pane watch.
func TestAgentDetectedAfterStartIsFollowedWithoutPolling(t *testing.T) {
	task := ledger.Task{Name: "d1", Kind: "claude", PaneID: "w:p7", Brief: "go", CreatedAt: time.Now()}
	srv, _ := setup(t, task)
	time.Sleep(100 * time.Millisecond) // let the initial reconcile find nothing

	srv.SetAgent("d1", "w:p7", "blocked", "Do you trust this folder?")
	srv.Emit("pane.agent_detected", "w:p7", map[string]any{"workspace_id": "w", "agent": "claude"})
	eventually(t, "blocked recorded from agent_detected", func() bool {
		got, _ := ledger.Load("d1")
		return got.State == "blocked"
	})
	eventually(t, "needs-you notification", func() bool {
		_, n := srv.Snapshot()
		return len(n) == 1 && strings.Contains(n[0], "needs you")
	})

	srv.SetAgent("d1", "w:p7", "idle", "")
	srv.Emit("pane.agent_status_changed", "w:p7", map[string]any{"workspace_id": "w", "agent": "claude", "agent_status": "idle"})
	eventually(t, "brief delivered via status event", func() bool { p, _ := srv.Snapshot(); return len(p) == 1 })
}

// After leaving a blocked prompt the agent gets Settle time before the brief goes in.
func TestBriefWaitsForSettleAfterBlocked(t *testing.T) {
	task := ledger.Task{Name: "s1", Kind: "claude", PaneID: "w:p1", Brief: "go", CreatedAt: time.Now()}
	srv, _ := setupWith(t, task, func(s *Supervisor) { s.Settle = 300 * time.Millisecond })
	srv.SetAgent("s1", "w:p1", "blocked", "trust?")
	eventually(t, "needs-you notification", func() bool { _, n := srv.Snapshot(); return len(n) == 1 })

	srv.SetAgent("s1", "w:p1", "idle", "")
	srv.Emit("pane.agent_status_changed", "w:p1", map[string]any{"workspace_id": "w", "agent_status": "idle"})
	time.Sleep(100 * time.Millisecond)
	if p, _ := srv.Snapshot(); len(p) != 0 {
		t.Fatalf("brief sent before the agent settled: %v", p)
	}
	eventually(t, "brief delivered after settle", func() bool { p, _ := srv.Snapshot(); return len(p) == 1 })
}

// If the agent blocks again while settling, nothing is sent to it.
func TestBriefNotSentIfAgentBlocksWhileSettling(t *testing.T) {
	task := ledger.Task{Name: "s2", Kind: "claude", PaneID: "w:p1", Brief: "go", CreatedAt: time.Now()}
	srv, _ := setupWith(t, task, func(s *Supervisor) { s.Settle = 300 * time.Millisecond })
	srv.SetAgent("s2", "w:p1", "blocked", "trust?")
	eventually(t, "needs-you notification", func() bool { _, n := srv.Snapshot(); return len(n) == 1 })

	srv.SetAgent("s2", "w:p1", "idle", "")
	srv.Emit("pane.agent_status_changed", "w:p1", map[string]any{"workspace_id": "w", "agent_status": "idle"})
	time.Sleep(50 * time.Millisecond)
	srv.SetAgent("s2", "w:p1", "blocked", "another prompt")
	time.Sleep(500 * time.Millisecond)
	if p, _ := srv.Snapshot(); len(p) != 0 {
		t.Fatalf("brief sent to a blocked agent: %v", p)
	}
}

// An agent that is idle after delivery without the brief on screen warns the captain, once, and is never resent to.
func TestLostBriefWarnsCaptain(t *testing.T) {
	task := ledger.Task{Name: "v1", Kind: "claude", PaneID: "w:p1", Brief: "Review the welcome page carefully", CreatedAt: time.Now()}
	srv, _ := setupWith(t, task, func(s *Supervisor) { s.Verify = 200 * time.Millisecond })
	srv.SetAgent("v1", "w:p1", "idle", "")
	eventually(t, "brief delivered", func() bool { p, _ := srv.Snapshot(); return len(p) == 1 })
	srv.SetAgent("v1", "w:p1", "idle", "an empty prompt") // the brief never showed up
	eventually(t, "lost-brief warning", func() bool {
		_, n := srv.Snapshot()
		return len(n) == 1 && strings.Contains(n[0], "may not have its brief")
	})
	if p, _ := srv.Snapshot(); len(p) != 1 {
		t.Fatalf("brief was resent: %v", p)
	}
}

// A brief visible on screen (even soft-wrapped) means it arrived: no warning.
func TestReceivedBriefDoesNotWarn(t *testing.T) {
	task := ledger.Task{Name: "v2", Kind: "claude", PaneID: "w:p1", Brief: "Review the welcome page carefully", CreatedAt: time.Now()}
	srv, _ := setupWith(t, task, func(s *Supervisor) { s.Verify = 100 * time.Millisecond })
	srv.SetAgent("v2", "w:p1", "idle", "")
	eventually(t, "brief delivered", func() bool { p, _ := srv.Snapshot(); return len(p) == 1 })
	srv.SetAgent("v2", "w:p1", "idle", "> Review the welcome\n  page carefully")
	time.Sleep(400 * time.Millisecond)
	if _, n := srv.Snapshot(); len(n) != 0 {
		t.Fatalf("unexpected notifications: %v", n)
	}
}

// A long brief pasted into the input box but never submitted shows as a
// placeholder there; that is not a received brief.
func TestUnsubmittedPastedBriefWarns(t *testing.T) {
	task := ledger.Task{Name: "v3", Kind: "claude", PaneID: "w:p1", Brief: "Review the welcome page carefully", CreatedAt: time.Now()}
	srv, _ := setupWith(t, task, func(s *Supervisor) { s.Verify = 200 * time.Millisecond })
	srv.SetAgent("v3", "w:p1", "idle", "")
	eventually(t, "brief delivered", func() bool { p, _ := srv.Snapshot(); return len(p) == 1 })
	srv.SetAgent("v3", "w:p1", "idle", "---\n❯ [Pasted text #1 +8 lines]\n---\n  fleet-v3 | ctx: 0%")
	eventually(t, "unsent-brief warning", func() bool {
		_, n := srv.Snapshot()
		return len(n) == 1 && strings.Contains(n[0], "may not have its brief")
	})
}

// A placeholder in the transcript, with the input box empty again, means the
// pasted brief was submitted.
func TestSubmittedPastedBriefDoesNotWarn(t *testing.T) {
	task := ledger.Task{Name: "v4", Kind: "claude", PaneID: "w:p1", Brief: "Review the welcome page carefully", CreatedAt: time.Now()}
	srv, _ := setupWith(t, task, func(s *Supervisor) { s.Verify = 100 * time.Millisecond })
	srv.SetAgent("v4", "w:p1", "idle", "")
	eventually(t, "brief delivered", func() bool { p, _ := srv.Snapshot(); return len(p) == 1 })
	srv.SetAgent("v4", "w:p1", "idle", "❯ [Pasted text #1 +8 lines]\n\n● done\n---\n❯ \n---\n  fleet-v4 | ctx: 0%")
	time.Sleep(400 * time.Millisecond)
	if _, n := srv.Snapshot(); len(n) != 0 {
		t.Fatalf("unexpected notifications: %v", n)
	}
}

// --- automatic disposal of finished crewmates ---

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// crewRepo makes a repo on main, plus a linked worktree on branch fleet/x with one commit of its own.
func crewRepo(t *testing.T) (repo, wt, baseRev string) {
	t.Helper()
	repo = t.TempDir()
	run(t, repo, "init", "-q", "-b", "main")
	os.WriteFile(repo+"/a.txt", []byte("a\n"), 0o644)
	run(t, repo, "add", ".")
	run(t, repo, "commit", "-q", "-m", "base")
	baseRev = run(t, repo, "rev-parse", "HEAD")
	wt = t.TempDir() + "/wt"
	run(t, repo, "worktree", "add", "-q", "-b", "fleet/x", wt)
	os.WriteFile(wt+"/b.txt", []byte("b\n"), 0o644)
	run(t, wt, "add", ".")
	run(t, wt, "commit", "-q", "-m", "crew work")
	return repo, wt, baseRev
}

func disposeTask(repo, wt, baseRev string) ledger.Task {
	return ledger.Task{Name: "x", Kind: "claude", Repo: repo, Branch: "fleet/x", Base: "main", BaseRev: baseRev,
		Worktree: wt, WorkspaceID: "wx", PaneID: "w:p1", Brief: "Review the welcome page carefully", BriefSent: true, CreatedAt: time.Now()}
}

func removed(srv *fake.Server) []string { _, _, rm, _ := srv.Recorded(); return rm }

func TestMergedCrewmateIsDisposed(t *testing.T) {
	repo, wt, base := crewRepo(t)
	run(t, repo, "merge", "-q", "--no-ff", "fleet/x", "-m", "merge")
	srv, _ := setupWith(t, disposeTask(repo, wt, base), nil)
	srv.SetAgent("x", "w:p1", "idle", "")
	srv.SetWorkspace("w:p1", "wx")
	eventually(t, "worktree removed", func() bool { return len(removed(srv)) == 1 })
	if got, _ := ledger.Load("x"); got.State != "stopped" {
		t.Fatalf("state = %q", got.State)
	}
	eventually(t, "disposed notification", func() bool {
		_, n := srv.Snapshot()
		return len(n) > 0 && strings.Contains(n[len(n)-1], "disposed")
	})
}

func TestUnmergedCrewmateIsKept(t *testing.T) {
	repo, wt, base := crewRepo(t) // never merged
	srv, _ := setupWith(t, disposeTask(repo, wt, base), nil)
	srv.SetAgent("x", "w:p1", "idle", "")
	time.Sleep(300 * time.Millisecond)
	if rm := removed(srv); len(rm) != 0 {
		t.Fatalf("unmerged crewmate was disposed: %v", rm)
	}
}

// A crewmate that has not moved past its base must not count as merged (it would be
// disposed the moment it started).
func TestFreshCrewmateIsNotMerged(t *testing.T) {
	repo, wt, _ := crewRepo(t)
	tip := run(t, repo, "rev-parse", "fleet/x")
	srv, _ := setupWith(t, disposeTask(repo, wt, tip), nil)
	srv.SetAgent("x", "w:p1", "idle", "")
	time.Sleep(300 * time.Millisecond)
	if rm := removed(srv); len(rm) != 0 {
		t.Fatalf("fresh crewmate was disposed: %v", rm)
	}
}

func TestMergedButDirtyIsNotDisposed(t *testing.T) {
	repo, wt, base := crewRepo(t)
	run(t, repo, "merge", "-q", "--no-ff", "fleet/x", "-m", "merge")
	os.WriteFile(wt+"/wip.txt", []byte("unsaved\n"), 0o644)
	srv, _ := setupWith(t, disposeTask(repo, wt, base), nil)
	srv.SetAgent("x", "w:p1", "idle", "")
	eventually(t, "not-disposed notification", func() bool {
		_, n := srv.Snapshot()
		return len(n) == 1 && strings.Contains(n[0], "not disposed")
	})
	if rm := removed(srv); len(rm) != 0 {
		t.Fatalf("dirty worktree was disposed: %v", rm)
	}
}

func TestKeepFlagPreventsDisposal(t *testing.T) {
	repo, wt, base := crewRepo(t)
	run(t, repo, "merge", "-q", "--no-ff", "fleet/x", "-m", "merge")
	task := disposeTask(repo, wt, base)
	task.Keep = true
	srv, _ := setupWith(t, task, nil)
	srv.SetAgent("x", "w:p1", "idle", "")
	time.Sleep(300 * time.Millisecond)
	if rm := removed(srv); len(rm) != 0 {
		t.Fatalf("kept crewmate was disposed: %v", rm)
	}
}

// One-shot: disposed when its first turn ends, but only if it really received its brief.
func oneShot(t *testing.T, screen string) (*fake.Server, ledger.Task) {
	t.Helper()
	repo, wt, _ := crewRepo(t)
	run(t, wt, "reset", "-q", "--hard", "HEAD~1") // a report task made no commits; the worktree is clean
	task := ledger.Task{Name: "x", Kind: "claude", Repo: repo, Branch: "fleet/x", Worktree: wt, WorkspaceID: "wx", PaneID: "w:p1",
		Brief: "Review the welcome page carefully", BriefSent: true, OneShot: true, CreatedAt: time.Now()}
	srv, _ := setupWith(t, task, nil)
	srv.SetAgent("x", "w:p1", "working", "")
	srv.SetWorkspace("w:p1", "wx")
	srv.Emit("pane.agent_status_changed", "w:p1", map[string]any{"workspace_id": "wx", "agent_status": "working"})
	time.Sleep(100 * time.Millisecond)
	srv.SetAgent("x", "w:p1", "idle", screen)
	srv.Emit("pane.agent_status_changed", "w:p1", map[string]any{"workspace_id": "wx", "agent_status": "idle"})
	return srv, task
}

func TestOneShotIsDisposedWhenItsTurnEnds(t *testing.T) {
	srv, _ := oneShot(t, "> Review the welcome\n  page carefully\nHere is my report.")
	eventually(t, "one-shot disposed", func() bool { return len(removed(srv)) == 1 })
}

func TestOneShotWithoutItsBriefIsNotDisposed(t *testing.T) {
	srv, _ := oneShot(t, "an empty prompt")
	time.Sleep(400 * time.Millisecond)
	if rm := removed(srv); len(rm) != 0 {
		t.Fatalf("a crewmate that never got its brief was disposed: %v", rm)
	}
}

func TestFinishedTurnPromptsFirstMate(t *testing.T) {
	task := ledger.Task{Name: "m1", Kind: "claude", PaneID: "w:p2", Mate: "w:p1", Brief: "x", BriefSent: true, CreatedAt: time.Now()}
	srv, _ := setupWith(t, task, func(s *Supervisor) { s.Poll = 50 * time.Millisecond })
	srv.SetAgent("first-mate", "w:p1", "working", "")
	srv.SetAgent("m1", "w:p2", "working", "")
	eventually(t, "state working", func() bool { got, _ := ledger.Load("m1"); return got.State == "working" })

	srv.SetAgent("m1", "w:p2", "done", "all finished")
	srv.Emit("pane.agent_status_changed", "w:p2", map[string]any{"workspace_id": "w", "agent_status": "done"})
	eventually(t, "toast", func() bool { _, n := srv.Snapshot(); return len(n) == 1 })
	if p, _ := srv.Snapshot(); len(p) != 0 {
		t.Fatalf("busy first mate was prompted: %v", p)
	}

	// The mate finishes its turn; the next poll delivers the queued alert.
	srv.SetAgent("first-mate", "w:p1", "idle", "")
	eventually(t, "first mate prompted", func() bool {
		p, _ := srv.Snapshot()
		return len(p) == 1 && strings.HasPrefix(p[0], "first-mate: [fleet] fleet: m1 finished a turn")
	})
}

// --- audit of leftover workspaces ---

func auditTune(s *Supervisor) {
	s.Poll = 30 * time.Millisecond
	s.AuditEvery = 1
	s.SelfWS, s.SupLabel = "wsup", "fleet-supervisor"
}

func TestAuditReportsExtraSupervisorWorkspaceOnceToTheFirstMate(t *testing.T) {
	task := ledger.Task{Name: "au1", Kind: "claude", PaneID: "w:p2", WorkspaceID: "wtask", Mate: "w:p1", Brief: "x", BriefSent: true, CreatedAt: time.Now()}
	srv, _ := setupWith(t, task, auditTune)
	srv.SetAgent("first-mate", "w:p1", "idle", "")
	srv.SetAgent("au1", "w:p2", "working", "")
	srv.AddWorkspace("wsup", "fleet-supervisor") // this supervisor's own: fine
	srv.AddWorkspace("wold", "fleet-supervisor") // a leftover
	eventually(t, "leftover reported", func() bool {
		_, n := srv.Snapshot()
		return len(n) == 1 && strings.Contains(n[0], "leftover workspace wold")
	})
	eventually(t, "first mate told", func() bool {
		p, _ := srv.Snapshot()
		return len(p) == 1 && strings.Contains(p[0], "herdr workspace close wold")
	})
	time.Sleep(200 * time.Millisecond) // more audits run: still reported once
	if _, n := srv.Snapshot(); len(n) != 1 {
		t.Fatalf("notifications = %v", n)
	}
}

func TestAuditReportsWorkspaceOfStoppedTask(t *testing.T) {
	task := ledger.Task{Name: "au2", Kind: "claude", PaneID: "w:p2", WorkspaceID: "wdead", State: "stopped", Brief: "x", BriefSent: true, CreatedAt: time.Now()}
	srv, _ := setupWith(t, task, auditTune)
	srv.AddWorkspace("wdead", "au2")
	srv.AddWorkspace("wother", "somebody's own workspace") // unknown to fleet: left alone
	eventually(t, "stopped task's workspace reported", func() bool {
		_, n := srv.Snapshot()
		return len(n) == 1 && strings.Contains(n[0], "leftover workspace wdead")
	})
}

// A workspace that disappears between audits (a stop in progress) is not reported.
func TestAuditIgnoresWorkspaceThatGoesAway(t *testing.T) {
	task := ledger.Task{Name: "au3", Kind: "claude", PaneID: "w:p2", WorkspaceID: "wgone", State: "stopped", Brief: "x", BriefSent: true, CreatedAt: time.Now()}
	srv, _ := setupWith(t, task, func(s *Supervisor) {
		s.Poll = time.Hour
		s.AuditEvery = 1
	})
	srv.AddWorkspace("wgone", "au3")
	// The initial audit sees it once; closing it before a second audit means no report.
	srv.CloseWorkspace("wgone")
	time.Sleep(150 * time.Millisecond)
	if _, n := srv.Snapshot(); len(n) != 0 {
		t.Fatalf("notifications = %v", n)
	}
}
