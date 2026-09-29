package main

import (
	"strings"
	"testing"

	"github.com/orgolan/fleet/internal/herdr/fake"
	"github.com/orgolan/fleet/internal/supervisor"
)

func TestUpStartsSupervisorOnlyWhenNotRunning(t *testing.T) {
	home := t.TempDir()
	t.Setenv("FLEET_HOME", home)
	srv := fake.New(t)
	t.Setenv("HERDR_SOCKET_PATH", srv.Socket)

	if err := up(nil); err != nil {
		t.Fatal(err)
	}
	if len(srv.Workspaces) != 1 || srv.Workspaces[0]["focus"] != false || srv.Workspaces[0]["label"] != supervisorLabel {
		t.Fatalf("workspaces = %v", srv.Workspaces)
	}
	_, _, _, runs := srv.Recorded()
	if len(runs) != 1 || !strings.Contains(runs[0], "supervise") || !strings.Contains(runs[0], "FLEET_HOME=") || !strings.HasSuffix(runs[0], "enter") {
		t.Fatalf("runs = %v", runs)
	}

	l, err := supervisor.Acquire(home)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	if err := up(nil); err != nil {
		t.Fatal(err)
	}
	if len(srv.Workspaces) != 1 {
		t.Fatalf("second up created another workspace: %v", srv.Workspaces)
	}
}

func TestSuperviseRefusesSecondInstance(t *testing.T) {
	home := t.TempDir()
	t.Setenv("FLEET_HOME", home)
	l, err := supervisor.Acquire(home)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	err = supervise(nil)
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("err = %v", err)
	}
}

// A dead supervisor's workspace is closed before a new one opens, so restarts do not pile them up.
func TestUpClosesLeftoverSupervisorWorkspaces(t *testing.T) {
	t.Setenv("FLEET_HOME", t.TempDir())
	srv := fake.New(t)
	t.Setenv("HERDR_SOCKET_PATH", srv.Socket)
	srv.AddWorkspace("wold", supervisorLabel)
	srv.AddWorkspace("wmine", "somebody's own workspace")
	if err := up(nil); err != nil {
		t.Fatal(err)
	}
	if cl := srv.ClosedWorkspaces(); len(cl) != 1 || cl[0] != "wold" {
		t.Fatalf("closed = %v", cl)
	}
	if len(srv.Workspaces) != 1 {
		t.Fatalf("workspaces = %v", srv.Workspaces)
	}
}

func TestParseMemAvailable(t *testing.T) {
	mb, ok := parseMemAvailableMB("MemTotal: 8000000 kB\nMemAvailable:    1536000 kB\n")
	if !ok || mb != 1500 {
		t.Fatalf("mb = %d, ok = %v", mb, ok)
	}
	if _, ok := parseMemAvailableMB("nothing here"); ok {
		t.Fatal("parsed garbage")
	}
}
