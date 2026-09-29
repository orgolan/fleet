package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"

	"github.com/orgolan/fleet/internal/herdr"
)

func ping() error {
	var p herdr.Ping
	if err := herdr.New().Call("ping", nil, &p); err != nil {
		return err
	}
	fmt.Printf("herdr %s (protocol %d) at %s\n", p.Version, p.Protocol, herdr.SocketPath())
	return nil
}

func events() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	c := herdr.New()
	agents, err := c.Agents()
	if err != nil {
		return err
	}
	// Status events are per pane, so subscribe for each agent pane known now.
	// Panes that gain an agent later are not followed here (the supervisor does).
	subs := []herdr.Sub{{"type": "pane.exited"}, {"type": "pane.agent_detected"}}
	for _, a := range agents {
		subs = append(subs, herdr.Sub{"type": "pane.agent_status_changed", "pane_id": a.PaneID})
	}
	evs, errc := c.Subscribe(ctx, subs...)
	for ev := range evs {
		if ev.Kind == "pane.agent_status_changed" {
			var d herdr.AgentStatusChanged
			if json.Unmarshal(ev.Data, &d) == nil {
				agent := "-"
				if d.Agent != nil {
					agent = *d.Agent
				}
				fmt.Printf("%s %s %s\n", d.PaneID, agent, d.Status)
				continue
			}
		}
		fmt.Printf("%s %s\n", ev.Kind, ev.Data)
	}
	return <-errc
}
