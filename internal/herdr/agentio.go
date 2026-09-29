package herdr

// AgentRead returns the last lines of recent, soft-wrap-joined output of an agent.
func (c *Client) AgentRead(target string, lines int) (string, error) {
	var r struct {
		Read struct {
			Text string `json:"text"`
		} `json:"read"`
	}
	err := c.Call("agent.read", map[string]any{
		"target": target, "lines": lines, "source": "recent_unwrapped",
	}, &r)
	return r.Read.Text, err
}

// Notify shows a toast in the herdr UI. sound is "none", "done" or "request".
func (c *Client) Notify(title, body, sound string) error {
	return c.Call("notification.show", map[string]any{
		"title": title, "body": body, "sound": sound,
	}, nil)
}
