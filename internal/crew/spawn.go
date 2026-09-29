// Package crew spawns crewmates: an isolated git worktree, a herdr workspace on
// it, a named agent in its root pane, and the initial brief.
package crew

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/orgolan/fleet/internal/claudetrust"
	"github.com/orgolan/fleet/internal/herdr"
	"github.com/orgolan/fleet/internal/ledger"
)

// Herdr agent names must match this.
var nameRE = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// Spec describes one crewmate.
type Spec struct {
	Name    string   // unique live agent name
	Kind    string   // herdr agent kind: claude, codex, ...
	Repo    string   // path inside the git repo to work on
	Project string   // registered project name, recorded on the task
	Branch  string   // defaults to fleet/<name>
	Base    string   // optional base ref
	Brief   string   // initial prompt; empty starts the agent idle
	Args    []string // native agent arguments
	Trust   bool     // pass trust_repository for the worktree request
	Dirty   bool     // copy the repo's uncommitted changes into the new worktree
	// TrustClaude marks the new worktree trusted in Claude Code, because the captain
	// trusted the project when registering it.
	TrustClaude bool
}

// Spawn creates the worktree and workspace, starts the agent and sends the brief.
// On failure after the worktree exists, nothing is torn down: the error names
// what was created so the captain can inspect it.
func Spawn(c *herdr.Client, s Spec) (ledger.Task, error) {
	var zero ledger.Task
	if !nameRE.MatchString(s.Name) {
		return zero, fmt.Errorf("invalid name %q: must match %s", s.Name, nameRE)
	}
	if s.Kind == "" {
		return zero, fmt.Errorf("agent kind is required")
	}
	s.Branch = branchFor(s)
	repo, err := filepath.Abs(s.Repo)
	if err != nil {
		return zero, err
	}
	if _, err := os.Stat(repo); err != nil {
		return zero, err
	}
	if ok, err := ledger.Exists(s.Name); err != nil {
		return zero, err
	} else if ok {
		return zero, fmt.Errorf("task %q already exists", s.Name)
	}

	wt, err := c.WorktreeCreate(herdr.WorktreeCreateParams{
		CWD: repo, Branch: s.Branch, Base: s.Base, Label: s.Name, TrustRepository: s.Trust,
	})
	if err != nil {
		return zero, fmt.Errorf("create worktree: %w", err)
	}
	if s.TrustClaude && s.Kind == "claude" {
		// Best effort: if it fails the crewmate simply stops at the trust prompt for the captain.
		if err := claudetrust.Trust(wt.Worktree.Path); err != nil {
			fmt.Fprintf(os.Stderr, "fleet: warning: could not pre-trust %s in Claude Code: %v\n", wt.Worktree.Path, err)
		}
	}
	t := ledger.Task{
		Name: s.Name, Kind: s.Kind, Repo: repo, Project: s.Project, Branch: s.Branch, Worktree: wt.Worktree.Path,
		WorkspaceID: wt.Workspace.WorkspaceID, PaneID: wt.RootPane.PaneID,
		Brief: s.Brief, CreatedAt: time.Now().UTC(),
	}
	// Record before starting the agent so a failure leaves a trace to reconcile.
	if err := ledger.Save(t); err != nil {
		return t, fmt.Errorf("worktree %s (workspace %s) created but not recorded: %w", t.Worktree, t.WorkspaceID, err)
	}
	if s.Dirty {
		if err := CopyDirty(repo, wt.Worktree.Path); err != nil {
			return t, fmt.Errorf("copy uncommitted changes (worktree %s, workspace %s left in place): %w", t.Worktree, t.WorkspaceID, err)
		}
	}
	if err := startWhenReady(c, s, t.PaneID); err != nil {
		return t, fmt.Errorf("start agent (worktree %s, workspace %s, pane %s left in place): %w", t.Worktree, t.WorkspaceID, t.PaneID, err)
	}
	if s.Brief != "" {
		if err := Deliver(c, s.Name, true, true); err != nil {
			if errors.Is(err, ErrBlocked) {
				return t, fmt.Errorf("agent %s is running but blocked at an approval or trust prompt (pane %s); the brief is recorded and NOT sent: resolve the prompt, and a running `fleet supervise` will deliver it, or run: herdr agent prompt %s %q", s.Name, t.PaneID, s.Name, s.Brief)
			}
			return t, fmt.Errorf("send brief to %s (agent is running; do not blindly resend): %w", s.Name, err)
		}
	}
	return t, nil
}

// branchFor is the crewmate's branch: the requested one, else fleet/<name>.
func branchFor(s Spec) string {
	if s.Branch != "" {
		return s.Branch
	}
	return "fleet/" + s.Name
}

// startWhenReady starts the agent, retrying while the new pane's shell is not
// yet at its prompt (herdr answers agent_pane_busy until it is).
func startWhenReady(c *herdr.Client, s Spec, paneID string) error {
	deadline := time.Now().Add(15 * time.Second)
	for {
		_, err := c.AgentStart(s.Name, s.Kind, paneID, s.Args)
		var he *herdr.Error
		if err == nil || !errors.As(err, &he) || he.Code != "agent_pane_busy" || time.Now().After(deadline) {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}
}
