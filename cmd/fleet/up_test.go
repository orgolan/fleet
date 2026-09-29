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
