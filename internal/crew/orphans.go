package crew

import (
	"fmt"
	"os"

	"github.com/orgolan/fleet/internal/herdr"
	"github.com/orgolan/fleet/internal/ledger"
)

// Orphan is an open workspace fleet no longer accounts for.
type Orphan struct {
	ID, Label, Why string
}

// Orphans picks out the workspaces nobody is using: the workspace of a task that
// has stopped or exited, a supervisor workspace other than selfWS, and the
// workspace herdr opened on the main checkout of a repo fleet has worked on when
// no live task is left there. Workspaces that match none of these (the captain's
// own) are never returned.
func Orphans(wss []herdr.Workspace, ts []ledger.Task, selfWS, supLabel string) []Orphan {
	ended := func(t ledger.Task) bool { return t.State == "exited" || t.State == "stopped" }
	endedWS := map[string]string{} // workspace id -> task name
	knownRepos, liveRepos := map[string]bool{}, map[string]bool{}
	for _, t := range ts {
		if ended(t) && t.WorkspaceID != "" {
			endedWS[t.WorkspaceID] = t.Name
		}
		if t.Repo != "" {
			knownRepos[t.Repo] = true
			if !ended(t) {
				liveRepos[t.Repo] = true
			}
		}
	}
	var out []Orphan
	for _, w := range wss {
		why := ""
		if task, ok := endedWS[w.WorkspaceID]; ok {
			why = "it still belongs to " + task + ", which fleet has stopped or lost"
		} else if supLabel != "" && selfWS != "" && w.Label == supLabel && w.WorkspaceID != selfWS {
			why = "it is an extra supervisor workspace; this supervisor runs in " + selfWS
		} else if wt := w.Worktree; wt != nil && !wt.IsLinked && knownRepos[wt.CheckoutPath] && !liveRepos[wt.CheckoutPath] {
			why = "herdr opens a workspace on a repo's main checkout when crewmates are created there, and none is using " + wt.CheckoutPath + " now"
		}
		if why != "" {
			out = append(out, Orphan{w.WorkspaceID, w.Label, why})
		}
	}
	return out
}

// CleanWorkspaces closes every orphaned workspace (see Orphans), or only lists them when
// dry is set. It never touches worktrees or task records. selfWS, when set, is
// the supervisor workspace to keep.
func CleanWorkspaces(c *herdr.Client, selfWS, supLabel string, dry bool) ([]Orphan, error) {
	wss, err := c.WorkspaceList()
	if err != nil {
		return nil, err
	}
	ts, err := ledger.List()
	if err != nil {
		return nil, err
	}
	orphans := Orphans(wss, ts, selfWS, supLabel)
	if dry {
		return orphans, nil
	}
	var closed []Orphan
	for _, o := range orphans {
		if err := c.WorkspaceClose(o.ID); err != nil {
			fmt.Fprintf(os.Stderr, "fleet: warning: could not close workspace %s: %v\n", o.ID, err)
			continue
		}
		closed = append(closed, o)
	}
	return closed, nil
}
