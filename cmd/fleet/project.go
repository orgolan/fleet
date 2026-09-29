package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"fleet/internal/projects"
)

const projectUsage = `usage: fleet project <subcommand>

  add <name> <path> [--base REF]   put the git repo containing <path> in scope
  list [--json]                    registered projects
  show <name>                      project record and notes
  note <name> <text...>            append a dated line to the project's notes
  rm <name>                        unregister (the repo itself is untouched)
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
		p, err := projects.Get(rest[0])
		if err != nil {
			return err
		}
		notes, err := projects.Notes(p.Name)
		if err != nil {
			return err
		}
		fmt.Printf("name: %s\npath: %s\nbase: %s\n\n%s", p.Name, p.Path, p.Base, notes)
		return nil
	case "note":
		if len(rest) < 2 {
			return fmt.Errorf("usage: fleet project note <name> <text...>")
		}
		return projects.AddNote(rest[0], strings.Join(rest[1:], " "))
	case "rm":
		if len(rest) != 1 {
			return fmt.Errorf("usage: fleet project rm <name>")
		}
		if err := projects.Remove(rest[0]); err != nil {
			return err
		}
		fmt.Printf("removed %s\n", rest[0])
		return nil
	}
	return fmt.Errorf("unknown project subcommand %q\n%s", args[0], strings.TrimRight(projectUsage, "\n"))
}
