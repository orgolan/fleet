package ledger

import (
	"testing"
	"time"
)

// A long-held action lock (brief delivery) must not stall or lose state updates.
func TestUpdateDoesNotWaitOnActionLock(t *testing.T) {
	t.Setenv("FLEET_HOME", t.TempDir())
	if err := Save(Task{Name: "x", Brief: "b"}); err != nil {
		t.Fatal(err)
	}
	held, release := make(chan struct{}), make(chan struct{})
	go WithLock("x", true, func() error { close(held); <-release; return nil })
	<-held
	defer close(release)

	done := make(chan error, 1)
	go func() { done <- Update("x", func(tk *Task) { tk.State = "blocked" }) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Update blocked on the action lock")
	}
	if got, _ := Load("x"); got.State != "blocked" || got.Brief != "b" {
		t.Fatalf("got %+v", got)
	}
}

func TestUpdatesDoNotClobberEachOther(t *testing.T) {
	t.Setenv("FLEET_HOME", t.TempDir())
	if err := Save(Task{Name: "y"}); err != nil {
		t.Fatal(err)
	}
	a, b := make(chan error), make(chan error)
	go func() { a <- Update("y", func(tk *Task) { tk.BriefSent = true }) }()
	go func() { b <- Update("y", func(tk *Task) { tk.State = "idle" }) }()
	if err := <-a; err != nil {
		t.Fatal(err)
	}
	if err := <-b; err != nil {
		t.Fatal(err)
	}
	if got, _ := Load("y"); !got.BriefSent || got.State != "idle" {
		t.Fatalf("lost update: %+v", got)
	}
}
