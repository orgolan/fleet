package supervisor

import (
	"context"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"fleet/internal/crew"
	"fleet/internal/herdr"
	"fleet/internal/herdr/fake"
	"fleet/internal/ledger"
)

func setup(t *testing.T, task ledger.Task) (*fake.Server, context.CancelFunc) {
	t.Helper()
	t.Setenv("FLEET_HOME", t.TempDir())
	if err := ledger.Save(task); err != nil {
		t.Fatal(err)
	}
	srv := fake.New(t)
	s := New(&herdr.Client{Socket: srv.Socket}, log.New(io.Discard, "", 0))
	s.Poll = time.Hour // events only: the reconcile tick must not mask event bugs
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
