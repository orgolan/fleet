package ledger

import (
	"bytes"
	"os"
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

// A record written by a newer fleet must survive an update by an older one.
func TestUpdateKeepsFieldsItDoesNotKnow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("FLEET_HOME", home)
	if err := Save(Task{Name: "x", Brief: "b"}); err != nil {
		t.Fatal(err)
	}
	p := home + "/tasks/x.json"
	raw, _ := os.ReadFile(p)
	raw = bytes.Replace(raw, []byte("{"), []byte(`{"from_the_future": {"a": [1, 2]},`), 1)
	os.WriteFile(p, raw, 0o644)

	if err := Update("x", func(tk *Task) { tk.State = "working" }); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if !bytes.Contains(got, []byte("from_the_future")) || !bytes.Contains(got, []byte(`"working"`)) {
		t.Fatalf("unknown field lost or update missing:\n%s", got)
	}
	if tk, _ := Load("x"); tk.State != "working" || tk.Brief != "b" {
		t.Fatalf("Load = %+v", tk)
	}
}
