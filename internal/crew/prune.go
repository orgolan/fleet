package crew

import (
	"time"

	"github.com/orgolan/fleet/internal/ledger"
)

// Prune deletes the records of tasks that were stopped more than olderThan ago,
// together with their saved output, and returns them. Only stopped tasks are
// eligible: a live or exited one may still own a worktree. With dryRun it only
// reports. The age counts from the record's last update, i.e. the stop.
func Prune(olderThan time.Duration, dryRun bool, now time.Time) ([]ledger.Task, error) {
	ts, err := ledger.List()
	if err != nil {
		return nil, err
	}
	var out []ledger.Task
	for _, t := range ts {
		if t.State != "stopped" {
			continue
		}
		stamp := t.UpdatedAt
		if stamp.IsZero() {
			stamp = t.CreatedAt
		}
		if now.Sub(stamp) < olderThan {
			continue
		}
		if !dryRun {
			if err := ledger.Remove(t.Name); err != nil {
				return out, err
			}
		}
		out = append(out, t)
	}
	return out, nil
}
