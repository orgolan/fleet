package supervisor

import (
	"context"
	"io"
	"log"
	"strings"
	"testing"
	"time"

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
