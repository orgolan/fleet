package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"fleet/internal/crew"
	"fleet/internal/herdr"
	"fleet/internal/ledger"
	"fleet/internal/projects"
)

func spawn(args []string) error {
	fs := flag.NewFlagSet("spawn", flag.ContinueOnError)
	spec := crew.Spec{}
	fs.StringVar(&spec.Kind, "kind", "claude", "herdr agent kind")
	fs.StringVar(&spec.Repo, "repo", ".", "path inside the git repo")
	proj := fs.String("project", "", "registered project (see fleet project); sets repo and base, adds its notes to the brief")
	fs.StringVar(&spec.Branch, "branch", "", "branch name (default fleet/<name>)")
	fs.StringVar(&spec.Base, "base", "", "base ref for the new branch")
	fs.BoolVar(&spec.Trust, "trust-repository", false, "grant per-request git trust (only for repos you verified)")
	// Everything after a literal "--" is passed to the agent as native arguments.
	for i, a := range args {
		if a == "--" {
			spec.Args = args[i+1:]
			args = args[:i]
			break
		}
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: fleet spawn [flags] <name> [brief...] [-- agent-args...]")
	}
	spec.Name = fs.Arg(0)
	spec.Brief = strings.Join(fs.Args()[1:], " ")
	if *proj != "" {
		if err := applyProject(&spec, *proj); err != nil {
			return err
		}
	}
	t, err := crew.Spawn(herdr.New(), spec)
	if t.Name != "" {
		// The crewmate is recorded (even if blocked or failed to start): make sure
		// a supervisor exists to deliver its brief and report on it.
		if started, pane, _, serr := ensureSupervisor(); serr != nil {
			fmt.Fprintln(os.Stderr, "fleet: warning: could not start supervisor:", serr)
		} else if started {
			fmt.Fprintf(os.Stderr, "fleet: started supervisor in workspace %q (pane %s)\n", supervisorLabel, pane)
		}
		// Print the task as stored (with its updated_at), not Spawn's copy.
		if stored, lerr := ledger.Load(t.Name); lerr == nil {
			t = stored
		}
	}
	if err != nil {
		if t.Name != "" {
			printJSON(t)
		}
		return err
	}
	return printJSON(t)
}

// applyProject resolves --project into repo, base and the brief's notes section.
func applyProject(spec *crew.Spec, name string) error {
	p, err := projects.Get(name)
	if err != nil {
		return err
	}
	spec.Repo = p.Path
	if spec.Base == "" {
		spec.Base = p.Base
	}
	notes, err := projects.Notes(name)
	if err != nil {
		return err
	}
	if notes = strings.TrimSpace(notes); notes != "" && spec.Brief != "" {
		spec.Brief += "\n\nProject notes (" + name + "):\n" + notes
	}
	return nil
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

func status(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print a JSON array")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rows, err := crew.Status(herdr.New())
	if err != nil {
		return err
	}
	return writeStatus(os.Stdout, rows, *asJSON)
}

func send(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: fleet send <name> <text...>")
	}
	return crew.Send(herdr.New(), args[0], strings.Join(args[1:], " "))
}

func read(args []string) error {
	fs := flag.NewFlagSet("read", flag.ContinueOnError)
	lines := fs.Int("lines", 40, "number of trailing lines")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 || *lines < 1 {
		return fmt.Errorf("usage: fleet read <name> [--lines N]")
	}
	txt, err := herdr.New().AgentRead(pos[0], *lines)
	if err != nil {
		return err
	}
	fmt.Println(strings.TrimRight(txt, "\n"))
	return nil
}

// keys is the captain's way to answer a blocked prompt; fleet never does it itself.
func keys(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: fleet keys <name> <key...>  (e.g. enter, esc, ctrl+c)")
	}
	return herdr.New().AgentSendKeys(args[0], args[1:])
}

func focus(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: fleet focus <name>")
	}
	return herdr.New().AgentFocus(args[0])
}

func stop(args []string) error {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	force := fs.Bool("force", false, "remove even a dirty or unmerged worktree, discarding uncommitted work")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("usage: fleet stop <name> [--force]")
	}
	if err := crew.Stop(herdr.New(), pos[0], *force); err != nil {
		return err
	}
	fmt.Printf("stopped %s\n", pos[0])
	return nil
}

func writeStatus(w *os.File, rows []crew.Row, asJSON bool) error {
	if asJSON {
		return printJSON(rows)
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tKIND\tSTATE\tLIVE\tWORKSPACE\tPANE\tBRIEF")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.Name, r.Kind, r.State, r.Live, r.WorkspaceID, r.PaneID, r.Brief)
	}
	return tw.Flush()
}
