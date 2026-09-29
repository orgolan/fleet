// Command fleet coordinates a crew of coding agents on top of herdr.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"fleet/internal/crew"
	"fleet/internal/herdr"
	"fleet/internal/ledger"
)

const usage = `usage: fleet <command>

commands:
  ping     check the herdr socket and print server version
  events   stream agent status changes (Ctrl+C to stop)
  spawn    start a crewmate in its own worktree: fleet spawn [flags] <name> [brief...]
  tasks    list recorded tasks
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "ping":
		err = ping()
	case "events":
		err = events()
	case "spawn":
		err = spawn(os.Args[2:])
	case "tasks":
		err = tasks()
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fleet:", err)
		os.Exit(1)
	}
}

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
	// TODO(supervisor): follow pane.agent_detected and resubscribe for new agents.
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

func spawn(args []string) error {
	fs := flag.NewFlagSet("spawn", flag.ContinueOnError)
	spec := crew.Spec{}
	fs.StringVar(&spec.Kind, "kind", "claude", "herdr agent kind")
	fs.StringVar(&spec.Repo, "repo", ".", "path inside the git repo")
	fs.StringVar(&spec.Branch, "branch", "", "branch name (default fleet/<name>)")
	fs.StringVar(&spec.Base, "base", "", "base ref for the new branch")
	fs.BoolVar(&spec.Trust, "trust-repository", false, "grant per-request git trust (only for repos you verified)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: fleet spawn [flags] <name> [brief...]")
	}
	spec.Name = fs.Arg(0)
	spec.Brief = strings.Join(fs.Args()[1:], " ")
	t, err := crew.Spawn(herdr.New(), spec)
	if err != nil {
		if t.Name != "" {
			printJSON(t)
		}
		return err
	}
	return printJSON(t)
}

func tasks() error {
	ts, err := ledger.List()
	if err != nil {
		return err
	}
	for _, t := range ts {
		fmt.Printf("%s\t%s\t%s\t%s\t%s\n", t.Name, t.Kind, t.WorkspaceID, t.PaneID, t.Worktree)
	}
	return nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
