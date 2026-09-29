package herdr

// AgentStatus is herdr's lifecycle state for a recognized agent.
type AgentStatus string

const (
	Idle    AgentStatus = "idle"
	Working AgentStatus = "working"
	Blocked AgentStatus = "blocked"
	Done    AgentStatus = "done"
	Unknown AgentStatus = "unknown"
)

// Ping is the result of the "ping" method.
type Ping struct {
	Type     string `json:"type"`
	Version  string `json:"version"`
	Protocol int    `json:"protocol"`
}

// AgentStatusChanged is the data of a pane.agent_status_changed event.
type AgentStatusChanged struct {
	PaneID      string      `json:"pane_id"`
	WorkspaceID string      `json:"workspace_id"`
	Agent       *string     `json:"agent"`
	Status      AgentStatus `json:"agent_status"`
	Title       *string     `json:"title"`
}

// Agent is one entry of "agent.list".
type Agent struct {
	Agent       string      `json:"agent"`
	Status      AgentStatus `json:"agent_status"`
	PaneID      string      `json:"pane_id"`
	WorkspaceID string      `json:"workspace_id"`
	CWD         string      `json:"cwd"`
}

// Agents lists the agents herdr currently recognizes.
func (c *Client) Agents() ([]Agent, error) {
	var r struct {
		Agents []Agent `json:"agents"`
	}
	err := c.Call("agent.list", nil, &r)
	return r.Agents, err
}
