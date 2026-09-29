package crew

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/orgolan/fleet/internal/herdr"
	"github.com/orgolan/fleet/internal/herdr/fake"
	"github.com/orgolan/fleet/internal/ledger"
)

func env(t *testing.T) (*fake.Server, *herdr.Client) {
	t.Helper()
	t.Setenv("FLEET_HOME", t.TempDir())
	srv := fake.New(t)
	return srv, &herdr.Client{Socket: srv.Socket}
}

func save(t *testing.T, task ledger.Task) {
	t.Helper()
	task.CreatedAt = time.Now()
	if err := ledger.Save(task); err != nil {
		t.Fatal(err)
	}
}

func TestStatusMergesLiveAndNot(t *testing.T) {
	srv, c := env(t)
	save(t, ledger.Task{Name: "a", Kind: "claude", PaneID: "w:p1", WorkspaceID: "w", Brief: "x", BriefSent: true, State: "working"})
	save(t, ledger.Task{Name: "b", Kind: "codex", PaneID: "w:p2", WorkspaceID: "w2", Brief: "y", State: "exited"})
	save(t, ledger.Task{Name: "c", Kind: "codex", PaneID: "w:p3", WorkspaceID: "w3"})
	srv.SetAgent("a", "w:p1", "idle", "")

	rows, err := Status(c)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Row{}
	for _, r := range rows {
		got[r.Name] = r
	}
	if r := got["a"]; r.Live != "idle" || r.State != "working" || r.Brief != "sent" {
		t.Errorf("a = %+v", r)
	}
	if r := got["b"]; r.Live != "-" || r.State != "exited" || r.Brief != "pending" {
		t.Errorf("b = %+v", r)
	}
	if r := got["c"]; r.Live != "-" || r.State != "-" || r.Brief != "-" {
		t.Errorf("c = %+v", r)
	}
}

func TestSendRefusesWhenBlocked(t *testing.T) {
	srv, c := env(t)
	srv.SetAgent("a", "w:p1", "blocked", "trust?")
	err := Send(c, "a", "hello")
	if !errors.Is(err, ErrBlocked) || !strings.Contains(err.Error(), "fleet read a") {
		t.Fatalf("err = %v", err)
	}
	if p, _ := srv.Snapshot(); len(p) != 0 {
		t.Fatalf("prompt sent to blocked agent: %v", p)
	}
	srv.SetAgent("a", "w:p1", "idle", "")
	if err := Send(c, "a", "hello"); err != nil {
		t.Fatal(err)
	}
	if p, _ := srv.Snapshot(); len(p) != 1 || p[0] != "a: hello" {
		t.Fatalf("prompts = %v", p)
	}
}

func TestKeysPassthrough(t *testing.T) {
	srv, c := env(t)
	srv.SetAgent("a", "w:p1", "blocked", "")
	if err := c.AgentSendKeys("a", []string{"ctrl+c", "enter"}); err != nil {
		t.Fatal(err)
	}
	if k, _, _, _ := srv.Recorded(); len(k) != 1 || k[0] != "a: ctrl+c,enter" {
		t.Fatalf("keys = %v", k)
	}
}

func TestStopRemovesWorktreeAndRecords(t *testing.T) {
	srv, c := env(t)
	save(t, ledger.Task{Name: "a", PaneID: "w:p1", WorkspaceID: "wa", State: "working"})
	srv.SetAgent("a", "w:p1", "working", "")
	srv.SetWorkspace("w:p1", "wa")
	if err := Stop(c, "a", false); err != nil {
		t.Fatal(err)
	}
	if _, _, rm, _ := srv.Recorded(); len(rm) != 1 || rm[0] != "wa" {
		t.Fatalf("removed = %v", rm)
	}
	if got, _ := ledger.Load("a"); got.State != "stopped" {
		t.Fatalf("state = %q", got.State)
	}
	if err := Stop(c, "a", false); err == nil {
		t.Fatal("second stop should fail")
	}
}

func TestStopDirtyNeedsForce(t *testing.T) {
	srv, c := env(t)
	save(t, ledger.Task{Name: "a", PaneID: "w:p1", WorkspaceID: "wa", State: "idle"})
	srv.SetDirty("wa")
	err := Stop(c, "a", false)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := ledger.Load("a"); got.State != "idle" {
		t.Fatalf("state not restored: %q", got.State)
	}
	if err := Stop(c, "a", true); err != nil {
		t.Fatal(err)
	}
	if _, _, rm, _ := srv.Recorded(); len(rm) != 1 || rm[0] != "wa!" {
		t.Fatalf("removed = %v", rm)
	}
}

// A worktree that git no longer knows (half-removed after an earlier failure)
// must not leave the crewmate running: close the workspace and mark it stopped.
func TestStopClosesWorkspaceWhenWorktreeIsGone(t *testing.T) {
	srv, c := env(t)
	save(t, ledger.Task{Name: "a", PaneID: "w:p1", WorkspaceID: "wa", State: "idle"})
	srv.SetAgent("a", "w:p1", "idle", "")
	srv.SetWorkspace("w:p1", "wa")
	srv.SetGone("wa")
	if err := Stop(c, "a", false); err != nil {
		t.Fatal(err)
	}
	if cl := srv.ClosedWorkspaces(); len(cl) != 1 || cl[0] != "wa" {
		t.Fatalf("closed = %v", cl)
	}
	if got, _ := ledger.Load("a"); got.State != "stopped" {
		t.Fatalf("state = %q", got.State)
	}
}

// A crewmate that ran wp-env leaves containers behind unless stop shuts them down.
func TestStopStopsWpEnvOnlyWhenTheWorktreeHasOne(t *testing.T) {
	var stopped []string
	old := stopEnv
	stopEnv = func(dir string) error { stopped = append(stopped, dir); return nil }
	t.Cleanup(func() { stopEnv = old })

	srv, c := env(t)
	withEnv, plain := t.TempDir(), t.TempDir()
	os.WriteFile(withEnv+"/.wp-env.json", []byte("{}"), 0o644)
	save(t, ledger.Task{Name: "a", PaneID: "w:p1", WorkspaceID: "wa", Worktree: withEnv, State: "idle"})
	save(t, ledger.Task{Name: "b", PaneID: "w:p2", WorkspaceID: "wb", Worktree: plain, State: "idle"})
	srv.SetAgent("a", "w:p1", "idle", "")
	srv.SetAgent("b", "w:p2", "idle", "")
	if err := Stop(c, "a", false); err != nil {
		t.Fatal(err)
	}
	if err := Stop(c, "b", false); err != nil {
		t.Fatal(err)
	}
	if len(stopped) != 1 || stopped[0] != withEnv {
		t.Fatalf("wp-env stopped in %v, want only %s", stopped, withEnv)
	}
}

func TestStopKeepsOutputAndResultFallsBackToIt(t *testing.T) {
	srv, c := env(t)
	save(t, ledger.Task{Name: "a", PaneID: "w:p1", WorkspaceID: "wa", State: "idle"})
	srv.SetAgent("a", "w:p1", "idle", "final report: all good")
	srv.SetWorkspace("w:p1", "wa")
	if txt, src, err := Result(c, "a", 50); err != nil || src != "live" || !strings.Contains(txt, "final report") {
		t.Fatalf("live Result = %q, %q, %v", txt, src, err)
	}
	if err := Stop(c, "a", false); err != nil {
		t.Fatal(err)
	}
	srv.RemoveAgent("w:p1") // the pane is gone after stop
	txt, src, err := Result(c, "a", 50)
	if err != nil || !strings.Contains(txt, "final report") || !strings.Contains(src, "saved") {
		t.Fatalf("saved Result = %q, %q, %v", txt, src, err)
	}
	if _, _, err := Result(c, "nope", 50); err == nil {
		t.Fatal("Result of an unknown task succeeded")
	}
}
