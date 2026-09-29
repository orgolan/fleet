package herdr

// AgentSendKeys sends named keys (enter, esc, ctrl+c, ...) to an agent's pane.
func (c *Client) AgentSendKeys(target string, keys []string) error {
	return c.Call("agent.send_keys", map[string]any{"target": target, "keys": keys}, nil)
}

// AgentFocus focuses an agent's pane in the herdr UI.
func (c *Client) AgentFocus(target string) error {
	return c.Call("agent.focus", map[string]any{"target": target}, nil)
}

// WorktreeRemove removes the linked worktree behind a workspace. Without
// force herdr refuses a dirty or unmerged worktree; force discards its work.
func (c *Client) WorktreeRemove(workspaceID string, force bool) error {
	return c.Call("worktree.remove", map[string]any{"workspace_id": workspaceID, "force": force}, nil)
}

// WorkspaceClose closes a workspace and its panes without touching any worktree.
func (c *Client) WorkspaceClose(workspaceID string) error {
	return c.Call("workspace.close", map[string]any{"workspace_id": workspaceID}, nil)
}

// WorkspaceList returns every open workspace.
func (c *Client) WorkspaceList() ([]Workspace, error) {
	var r struct {
		Workspaces []Workspace `json:"workspaces"`
	}
	err := c.Call("workspace.list", map[string]any{}, &r)
	return r.Workspaces, err
}

// WorkspaceCreated is the workspace.create result: the new workspace and its root pane.
type WorkspaceCreated struct {
	Workspace Workspace `json:"workspace"`
	RootPane  Pane      `json:"root_pane"`
}

// WorkspaceCreate opens a workspace without focusing it.
func (c *Client) WorkspaceCreate(cwd, label string) (WorkspaceCreated, error) {
	var r WorkspaceCreated
	err := c.Call("workspace.create", map[string]any{"cwd": cwd, "label": label, "focus": false}, &r)
	return r, err
}

// PaneRun types a shell command into a pane and presses enter. The socket API
// has no dedicated run method (the `herdr pane run` CLI does the same), so
// this is pane.send_input with text plus an enter key.
func (c *Client) PaneRun(paneID, command string) error {
	return c.Call("pane.send_input", map[string]any{
		"pane_id": paneID, "text": command, "keys": []string{"enter"},
	}, nil)
}
