package main

import (
	"strings"
	"testing"

	"fleet/internal/herdr/fake"
	"fleet/internal/supervisor"
)

func TestUpStartsSupervisorOnlyWhenNotRunning(t *testing.T) {
	home := t.TempDir()
	t.Setenv("FLEET_HOME", home)
	t.Setenv("HERDR_PANE_ID", "w:p1")
	srv := fake.New(t)
	t.Setenv("HERDR_SOCKET_PATH", srv.Socket)

	if err := up(nil); err != nil {
		t.Fatal(err)
	}
	if len(srv.Splits) != 1 || srv.Splits[0]["focus"] != false || srv.Splits[0]["target_pane_id"] != "w:p1" {
		t.Fatalf("splits = %v", srv.Splits)
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
	if len(srv.Splits) != 1 {
		t.Fatalf("second up split again: %v", srv.Splits)
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
