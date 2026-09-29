package herdr

// Pane is the subset of herdr's PaneInfo that fleet uses.
type Pane struct {
	PaneID      string `json:"pane_id"`
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	CWD         string `json:"cwd"`
}

// Workspace is the subset of herdr's WorkspaceInfo that fleet uses.
type Workspace struct {
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
}

// Worktree is herdr's WorktreeInfo.
type Worktree struct {
	Path   string  `json:"path"`
	Branch *string `json:"branch"`
	Label  string  `json:"label"`
}

// WorktreeCreateParams mirrors the worktree.create request.
type WorktreeCreateParams struct {
	CWD             string `json:"cwd,omitempty"`
	WorkspaceID     string `json:"workspace_id,omitempty"`
	Branch          string `json:"branch,omitempty"`
	Base            string `json:"base,omitempty"`
	Path            string `json:"path,omitempty"`
	Label           string `json:"label,omitempty"`
	Focus           bool   `json:"focus"`
	TrustRepository bool   `json:"trust_repository,omitempty"`
}

// WorktreeCreated is the worktree.create result: a new linked worktree, the
// workspace opened on it, and that workspace's root pane.
type WorktreeCreated struct {
	Workspace Workspace `json:"workspace"`
	RootPane  Pane      `json:"root_pane"`
	Worktree  Worktree  `json:"worktree"`
}

func (c *Client) WorktreeCreate(p WorktreeCreateParams) (WorktreeCreated, error) {
	var r WorktreeCreated
	err := c.Call("worktree.create", p, &r)
	return r, err
}

// AgentStart starts a named agent of the given kind in an available shell pane.
// args are native agent arguments. It returns once herdr sees the agent ready.
func (c *Client) AgentStart(name, kind, paneID string, args []string) (Agent, error) {
	if args == nil {
		args = []string{} // herdr rejects null
	}
	var r struct {
		Agent Agent `json:"agent"`
	}
	err := c.Call("agent.start", map[string]any{
		"name": name, "kind": kind, "pane_id": paneID, "args": args,
	}, &r)
	return r.Agent, err
}

// AgentPrompt submits text to a named agent without waiting for the turn.
func (c *Client) AgentPrompt(target, text string) error {
	return c.Call("agent.prompt", map[string]any{"target": target, "text": text}, nil)
}
