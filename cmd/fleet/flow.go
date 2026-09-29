package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/orgolan/fleet/internal/crew"
	"github.com/orgolan/fleet/internal/herdr"
	"github.com/orgolan/fleet/internal/ledger"
)

// wait blocks until crewmates have settled, so a first mate needs no polling loops.
func wait(args []string) error {
	fs := flag.NewFlagSet("wait", flag.ContinueOnError)
	all := fs.Bool("all", false, "wait for every named crewmate (default: return when the first settles)")
	timeout := fs.Duration("timeout", 30*time.Minute, "give up after this long (0 waits forever)")
	names, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	rows, err := crew.Wait(herdr.New(), names, *all, *timeout, 3*time.Second)
	if len(rows) > 0 {
		writeStatus(os.Stdout, rows, false)
	}
	if errors.Is(err, crew.ErrTimeout) {
		return fmt.Errorf("%w (after %s)", err, *timeout)
	}
	return err
}

// merge lands a finished crewmate's branch on its base and stops the crewmate.
func merge(args []string) error {
	fs := flag.NewFlagSet("merge", flag.ContinueOnError)
	test := fs.String("test", "", "shell command to run in the repo with the merge applied; a failure aborts the merge")
	keep := fs.Bool("keep-running", false, "leave the crewmate running after the merge")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("usage: fleet merge <name> [--test \"cmd\"] [--keep-running]")
	}
	res, err := crew.Merge(herdr.New(), pos[0], crew.MergeOpts{Test: *test, Keep: *keep})
	for _, w := range res.Warnings {
		fmt.Fprintln(os.Stderr, "fleet: warning:", w)
	}
	if err != nil {
		return err
	}
	if res.AlreadyMerged {
		fmt.Printf("%s was already merged\n", pos[0])
	} else {
		fmt.Printf("merged %s\n", pos[0])
	}
	if res.Stopped {
		fmt.Printf("stopped %s; its report is kept: fleet result %s\n", pos[0], pos[0])
	}
	return nil
}

// resume relaunches crewmates whose panes are gone, e.g. after herdr was restarted.
func resume(args []string) error {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	all := fs.Bool("all", false, "resume every task recorded as exited")
	names, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if *all {
		ts, err := ledger.List()
		if err != nil {
			return err
		}
		for _, t := range ts {
			if t.State == "exited" {
				names = append(names, t.Name)
			}
		}
	}
	if len(names) == 0 {
		return fmt.Errorf("usage: fleet resume <name>... | --all")
	}
	c := herdr.New()
	var failed int
	for _, n := range names {
		if err := crew.Resume(c, n); err != nil {
			fmt.Fprintf(os.Stderr, "fleet: %s: %v\n", n, err)
			failed++
			continue
		}
		fmt.Printf("resumed %s\n", n)
	}
	if started, _, _, err := ensureSupervisor(); err == nil && started {
		fmt.Fprintln(os.Stderr, "fleet: started supervisor")
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d could not be resumed", failed, len(names))
	}
	return nil
}
