package crew

import (
	"testing"
	"time"

	"github.com/orgolan/fleet/internal/ledger"
)

func TestPruneOnlyOldStoppedTasks(t *testing.T) {
	t.Setenv("FLEET_HOME", t.TempDir())
	now := time.Now()
	old := now.Add(-10 * 24 * time.Hour)
	mk := func(name, state string, at time.Time) {
		if err := ledger.Save(ledger.Task{Name: name, State: state, CreatedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	mk("old-stopped", "stopped", old)
	mk("new-stopped", "stopped", now)
	mk("old-working", "working", old)
	mk("old-exited", "exited", old)
	// Save stamps UpdatedAt with the real clock, so age comes from a later "now".
	later := now.Add(10 * 24 * time.Hour)
	ledger.SaveResult("old-stopped", "report")

	got, err := Prune(7*24*time.Hour, true, later)
	if err != nil || len(got) != 4-2 { // old-stopped and new-stopped are both >7d old at "later"
		t.Fatalf("dry run = %v, %v", names(got), err)
	}
	if ts, _ := ledger.List(); len(ts) != 4 {
		t.Fatal("dry run deleted records")
	}

	got, err = Prune(7*24*time.Hour, false, now.Add(24*time.Hour))
	if err != nil || len(got) != 0 {
		t.Fatalf("nothing is a week old yet, got %v, %v", names(got), err)
	}
	got, err = Prune(7*24*time.Hour, false, later)
	if err != nil {
		t.Fatal(err)
	}
	ts, _ := ledger.List()
	left := names(ts)
	if len(got) != 2 || len(left) != 2 || left[0] == "old-stopped" || left[0] == "new-stopped" || left[1] == "old-stopped" || left[1] == "new-stopped" {
		t.Fatalf("pruned %v, left %v", names(got), left)
	}
	if _, err := ledger.LoadResult("old-stopped"); err == nil {
		t.Fatal("saved output survived prune")
	}
}

func names(ts []ledger.Task) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Name)
	}
	return out
}
