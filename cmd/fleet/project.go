package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/orgolan/fleet/internal/ledger"
	"github.com/orgolan/fleet/internal/projects"
)

const projectUsage = `usage: fleet project <subcommand>

  new <name>                       create an empty git repo under projects/<name>/repo
  clone <name> <url>               clone a repo under projects/<name>/repo
  add <name> <path> [--base REF]   put an existing git repo (anywhere) in scope
  list [--json]                    registered projects
  show <name>                      record, numbered notes and the project's tasks
  note <name> [--conv] <text...>   add a note (a dated log entry; --conv: an always-sent convention)
  note <name> --edit N <text...>   replace note N (numbers are in "show")
  note <name> --rm N               delete note N
  rm <name> [--force]              unregister and drop notes; a repo made by new/clone is only deleted with --force
`

func project(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", strings.TrimRight(projectUsage, "\n"))
	}
	rest := args[1:]
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("project add", flag.ContinueOnError)
		base := fs.String("base", "", "default base ref for new branches")
		pos, err := parseInterspersed(fs, rest)
		if err != nil {
			return err
		}
		if len(pos) != 2 {
			return fmt.Errorf("usage: fleet project add <name> <path> [--base REF]")
		}
		p, err := projects.Add(pos[0], pos[1], *base)
		if err != nil {
			return err
		}
		fmt.Printf("added %s -> %s\n", p.Name, p.Path)
		return nil
	case "new":
		if len(rest) != 1 {
			return fmt.Errorf("usage: fleet project new <name>")
		}
		p, err := projects.New(rest[0])
		if err != nil {
			return err
		}
		fmt.Printf("created %s at %s\n", p.Name, p.Path)
		return nil
	case "clone":
		if len(rest) != 2 {
			return fmt.Errorf("usage: fleet project clone <name> <url>")
		}
		p, err := projects.Clone(rest[0], rest[1])
		if err != nil {
			return err
		}
		fmt.Printf("cloned %s to %s\n", p.Name, p.Path)
		return nil
	case "list":
		fs := flag.NewFlagSet("project list", flag.ContinueOnError)
		asJSON := fs.Bool("json", false, "print a JSON array")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		ps, err := projects.List()
		if err != nil {
			return err
		}
		if *asJSON {
			return printJSON(ps)
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tBASE\tPATH")
		for _, p := range ps {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", p.Name, p.Base, p.Path)
		}
		return tw.Flush()
	case "show":
		if len(rest) != 1 {
			return fmt.Errorf("usage: fleet project show <name>")
		}
		return showProject(rest[0])
	case "note":
		return noteCmd(rest)
	case "rm":
		fs := flag.NewFlagSet("project rm", flag.ContinueOnError)
		force := fs.Bool("force", false, "also delete a repo created by new/clone (destroys its work)")
		pos, err := parseInterspersed(fs, rest)
		if err != nil {
			return err
		}
		if len(pos) != 1 {
			return fmt.Errorf("usage: fleet project rm <name> [--force]")
		}
		if err := projects.Remove(pos[0], *force); err != nil {
			return err
		}
		fmt.Printf("removed %s\n", pos[0])
		return nil
	}
	return fmt.Errorf("unknown project subcommand %q\n%s", args[0], strings.TrimRight(projectUsage, "\n"))
}

func showProject(name string) error {
	p, err := projects.Get(name)
	if err != nil {
		return err
	}
	nf, err := projects.LoadNotes(name)
	if err != nil {
		return err
	}
	fmt.Printf("name: %s\npath: %s\nbase: %s\n", p.Name, p.Path, p.Base)
	var last projects.Section
	for _, e := range nf.Entries() {
		if e.Section != last {
			fmt.Printf("\n%s\n", e.Section)
			last = e.Section
		}
		fmt.Printf("  %2d  %s\n", e.Num, e.Text)
	}
	if last == "" {
		fmt.Println("\n(no notes yet: fleet project note " + name + " \"...\")")
	}
	if n := nf.Size(); n > projects.WarnChars {
		fmt.Printf("\nnotes are %d characters (more than %d): trim with `fleet project note %s --rm N`\n", n, projects.WarnChars, name)
	}
	ts, err := ledger.List()
	if err != nil {
		return err
	}
	var mine []ledger.Task
	for _, t := range ts {
		if t.Project == name {
			mine = append(mine, t)
		}
	}
	if len(mine) > 0 {
		fmt.Printf("\nTasks\n")
		for _, t := range mine {
			state := t.State
			if state == "" {
				state = "-"
			}
			brief, _, _ := strings.Cut(t.Brief, "\n")
			if r := []rune(brief); len(r) > 70 {
				brief = string(r[:70]) + "..."
			}
			fmt.Printf("  %-16s %-8s %s\n", t.Name, state, brief)
		}
	}
	return nil
}

func noteCmd(args []string) error {
	const usage = "usage: fleet project note <name> [--conv] <text...> | --edit N <text...> | --rm N"
	if len(args) < 2 {
		return fmt.Errorf(usage)
	}
	name := args[0]
	fs := flag.NewFlagSet("project note", flag.ContinueOnError)
	conv := fs.Bool("conv", false, "add a convention (always sent) instead of a log entry")
	edit := fs.Int("edit", 0, "replace note N")
	rm := fs.Int("rm", 0, "delete note N")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	text := strings.Join(fs.Args(), " ")
	nf, err := projects.LoadNotes(name)
	if err != nil {
		return err
	}
	switch {
	case *rm > 0 && *edit == 0 && text == "" && !*conv:
		err = nf.Remove(*rm)
	case *edit > 0 && *rm == 0 && !*conv:
		err = nf.Edit(*edit, text)
	case *rm == 0 && *edit == 0:
		sec := projects.Log
		if *conv {
			sec = projects.Conventions
		}
		err = nf.Add(sec, text)
	default:
		return fmt.Errorf(usage)
	}
	if err != nil {
		return err
	}
	if err := nf.Save(); err != nil {
		return err
	}
	if n := nf.Size(); n > projects.WarnChars {
		fmt.Fprintf(os.Stderr, "fleet: warning: notes are %d characters (more than %d); trim with `fleet project note %s --rm N`\n", n, projects.WarnChars, name)
	}
	return nil
}
