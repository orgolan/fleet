package crew

import (
	"errors"
	"fmt"
	"time"

	"fleet/internal/herdr"
	"fleet/internal/ledger"
)

// ErrBlocked means the agent is at an approval or trust prompt; the brief was not sent.
var ErrBlocked = errors.New("agent is blocked at an approval or trust prompt")

// Deliver sends a task's brief once. It takes the task lock, re-reads the record
// and does nothing if the brief was already sent, so the spawner and the
// supervisor can both call it safely. With wait, it retries while herdr has not
// yet recognized the new agent; without it, that case returns the error at once.
// If the lock is held elsewhere and block is false it returns ledger.ErrLocked.
func Deliver(c *herdr.Client, name string, wait, block bool) error {
	return ledger.WithLock(name, block, func() error {
		t, err := ledger.Load(name)
		if err != nil {
			return err
		}
		if t.BriefSent || t.Brief == "" {
			return nil
		}
		if wait {
			err = promptWhenReady(c, name, t.Brief)
		} else {
			err = c.AgentPrompt(name, t.Brief)
		}
		var he *herdr.Error
		if errors.As(err, &he) && he.Code == "agent_blocked" {
			return fmt.Errorf("%w (pane %s)", ErrBlocked, t.PaneID)
		}
		if err != nil {
			return err
		}
		return ledger.Update(name, func(t *ledger.Task) {
			t.BriefSent = true
			t.State = "working"
		})
	})
}

func promptWhenReady(c *herdr.Client, name, text string) error {
	deadline := time.Now().Add(30 * time.Second)
	for {
		err := c.AgentPrompt(name, text)
		var he *herdr.Error
		if err == nil || !errors.As(err, &he) || he.Code != "agent_not_ready" || time.Now().After(deadline) {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}
}
