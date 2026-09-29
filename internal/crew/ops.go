package crew

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/orgolan/fleet/internal/herdr"
	"github.com/orgolan/fleet/internal/ledger"
)

// Row is one task merged with what herdr reports for its pane.
type Row struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	State       string `json:"state"` // ledger state
	Live        string `json:"live"`  // herdr agent_status, "-" without a live agent
	WorkspaceID string `json:"workspace_id"`
	PaneID      string `json:"pane_id"`
	Brief       string `json:"brief"` // sent, pending, or "-" when there was none
}

// Status merges every ledger task with the live agent list, matching by pane.
func Status(c *herdr.Client) ([]Row, error) {
	ts, err := ledger.List()
	if err != nil {
		return nil, err
	}
	agents, err := c.Agents()
	if err != nil {
		return nil, fmt.Errorf("agent list: %w", err)
	}
	live := map[string]herdr.Agent{}
	for _, a := range agents {
		live[a.PaneID] = a
	}
	rows := make([]Row, 0, len(ts))
	for _, t := range ts {
		r := Row{Name: t.Name, Kind: t.Kind, State: t.State, Live: "-", WorkspaceID: t.WorkspaceID, PaneID: t.PaneID, Brief: "-"}
		if r.State == "" {
			r.State = "-"
		}
		if a, ok := live[t.PaneID]; ok {
			r.Live = string(a.Status)
		}
		switch {
		case t.BriefSent:
			r.Brief = "sent"
		case t.Brief != "":
			r.Brief = "pending"
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// Send prompts a crewmate without waiting for its turn. It never retries: a
// blocked agent is reported as ErrBlocked so the captain can look first.
func Send(c *herdr.Client, name, text string) error {
	err := c.AgentPrompt(name, text)
	var he *herdr.Error
	if errors.As(err, &he) && he.Code == "agent_blocked" {
		return fmt.Errorf("%w; run `fleet read %s` to see the prompt, then answer it with `fleet keys %s <key...>`", ErrBlocked, name, name)
	}
	return err
}

// Stop removes a task's worktree and marks it stopped. The ledger is marked
// first: removing the worktree closes the pane, and the supervisor would
// otherwise see that as the crewmate exiting on its own. If herdr refuses
// (dirty or unmerged worktree without force) the previous state is restored.
func Stop(c *herdr.Client, name string, force bool) error {
	t, err := ledger.Load(name)
	if err != nil {
		return fmt.Errorf("no such task %q: %w", name, err)
	}
	if t.State == "stopped" {
		return fmt.Errorf("task %q is already stopped", name)
	}
	// Keep the crewmate's last output: the pane, and the report in it, is about to go.
	// Best effort; an agent that is busy or already gone cannot be read.
	if txt, err := c.AgentRead(name, resultLines); err == nil && strings.TrimSpace(txt) != "" {
		if err := ledger.SaveResult(name, txt); err != nil {
			fmt.Fprintf(os.Stderr, "fleet: warning: could not save %s's output: %v\n", name, err)
		}
	}
	// A crewmate that ran its own wp-env would leave its containers behind.
	if _, err := os.Stat(filepath.Join(t.Worktree, ".wp-env.json")); err == nil {
		if err := stopEnv(t.Worktree); err != nil {
			fmt.Fprintf(os.Stderr, "fleet: warning: could not stop %s's wp-env (containers may still run; `docker ps`): %v\n", name, err)
		}
	}
	prev := t.State
	if err := ledger.Update(name, func(t *ledger.Task) { t.State = "stopped" }); err != nil {
		return err
	}
	restore := func(err error) error {
		if rerr := ledger.Update(name, func(t *ledger.Task) {
			if t.State == "stopped" {
				t.State = prev
			}
		}); rerr != nil {
			return fmt.Errorf("%w (also failed to restore ledger state: %v)", err, rerr)
		}
		return err
	}
	if err := c.WorktreeRemove(t.WorkspaceID, force); err != nil {
		// Git no longer knows the worktree (removed by hand, or a removal that failed
		// part way): there is no work left to protect, so just close the workspace.
		if strings.Contains(err.Error(), "is not a working tree") {
			if cerr := c.WorkspaceClose(t.WorkspaceID); cerr != nil {
				return restore(fmt.Errorf("%w (and closing its workspace failed: %v)", err, cerr))
			}
			fmt.Fprintf(os.Stderr, "fleet: warning: %s's worktree %s was already gone from git; closed its workspace. Delete the directory by hand if it is still there.\n", name, t.Worktree)
			return nil
		}
		err = restore(err)
		if !force {
			return fmt.Errorf("%w\nif the worktree is dirty or unmerged, commit/merge the work, or rerun with --force to DISCARD uncommitted work in %s; otherwise fix the error above", err, t.Worktree)
		}
		return err
	}
	return nil
}

// stopEnv stops the wp-env stack of a worktree; a variable so tests need no Docker.
var stopEnv = func(dir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "npx", "--no-install", "wp-env", "stop")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, firstLine(strings.TrimSpace(string(out))))
	}
	return nil
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}

// resultLines is how much output Stop keeps and Result reads.
const resultLines = 400

// Result returns a crewmate's recent output: live when it can be read, else the
// copy saved when it was stopped. The second value says which one it was.
func Result(c *herdr.Client, name string, lines int) (text, source string, err error) {
	if _, lerr := ledger.Load(name); lerr != nil {
		return "", "", fmt.Errorf("no such task %q: %w", name, lerr)
	}
	live, rerr := c.AgentRead(name, lines)
	if rerr == nil {
		return live, "live", nil
	}
	if saved, serr := ledger.LoadResult(name); serr == nil {
		return saved, "saved when stopped", nil
	}
	return "", "", fmt.Errorf("cannot read %s and no saved output exists: %w", name, rerr)
}
