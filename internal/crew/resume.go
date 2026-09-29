package crew

import (
	"fmt"
	"os"

	"github.com/orgolan/fleet/internal/herdr"
	"github.com/orgolan/fleet/internal/ledger"
)

const resumePrompt = "Your previous session ended unexpectedly (herdr or the agent stopped). " +
	"Carry on from where you were: check `git status` and `git log`, re-read .fleet/brief.md if it exists " +
	"for the original task, finish the work, commit it, and update .fleet/report.md."

// Resume relaunches a crewmate whose pane is gone (herdr was closed, the agent
// crashed) in its existing worktree, continuing the agent's last session there.
// Uncommitted work in the worktree is untouched: it is what the crewmate resumes.
func Resume(c *herdr.Client, name string) error {
	t, err := ledger.Load(name)
	if err != nil {
		return fmt.Errorf("no such task %q: %w", name, err)
	}
	if t.State == "stopped" {
		return fmt.Errorf("task %q was stopped and its worktree removed; spawn it again", name)
	}
	if agents, err := c.Agents(); err == nil {
		for _, a := range agents {
			if a.PaneID == t.PaneID {
				return fmt.Errorf("%s is still running (pane %s); nothing to resume", name, t.PaneID)
			}
		}
	}
	if _, err := os.Stat(t.Worktree); err != nil {
		return fmt.Errorf("%s's worktree %s is gone: %w", name, t.Worktree, err)
	}
	ws, err := c.WorkspaceCreate(t.Worktree, name)
	if err != nil {
		return fmt.Errorf("open a workspace on %s: %w", t.Worktree, err)
	}
	var args []string
	if t.Kind == "claude" {
		args = []string{"--continue"}
	}
	if err := startWhenReady(c, Spec{Name: name, Kind: t.Kind, Args: args}, ws.RootPane.PaneID); err != nil {
		return fmt.Errorf("start agent (workspace %s left open): %w", ws.Workspace.WorkspaceID, err)
	}
	old := t.WorkspaceID
	if err := ledger.Update(name, func(t *ledger.Task) {
		t.WorkspaceID, t.PaneID, t.State = ws.Workspace.WorkspaceID, ws.RootPane.PaneID, "working"
	}); err != nil {
		return err
	}
	if old != "" && old != ws.Workspace.WorkspaceID {
		c.WorkspaceClose(old) // best effort: the dead pane's workspace
	}
	if err := promptWhenReady(c, name, resumePrompt); err != nil {
		return fmt.Errorf("resumed, but could not send the resume prompt (send it with `fleet send %s`): %w", name, err)
	}
	return nil
}
