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

// PaneSplitParams mirrors the pane.split request. Direction is "right" or "down".
type PaneSplitParams struct {
	TargetPaneID string `json:"target_pane_id,omitempty"`
	Direction    string `json:"direction"`
	CWD          string `json:"cwd,omitempty"`
	Focus        bool   `json:"focus"`
}

// PaneSplit splits a pane and returns the new one.
func (c *Client) PaneSplit(p PaneSplitParams) (Pane, error) {
	var r struct {
		Pane Pane `json:"pane"`
	}
	err := c.Call("pane.split", p, &r)
	return r.Pane, err
}

// PaneRun types a shell command into a pane and presses enter. The socket API
// has no dedicated run method (the `herdr pane run` CLI does the same), so
// this is pane.send_input with text plus an enter key.
func (c *Client) PaneRun(paneID, command string) error {
	return c.Call("pane.send_input", map[string]any{
		"pane_id": paneID, "text": command, "keys": []string{"enter"},
	}, nil)
}
