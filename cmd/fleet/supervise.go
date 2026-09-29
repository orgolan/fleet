package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"time"

	"fleet/internal/herdr"
	"fleet/internal/ledger"
	"fleet/internal/supervisor"
)

func supervise(args []string) error {
	fs := flag.NewFlagSet("supervise", flag.ContinueOnError)
	poll := fs.Duration("poll", 20*time.Second, "reconcile interval (safety net for missed events)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := ledger.Dir()
	if err != nil {
		return err
	}
	lock, err := supervisor.Acquire(dir)
	if err != nil {
		if errors.Is(err, supervisor.ErrRunning) {
			return fmt.Errorf("%w; not starting a second one (lock: %s/supervisor.lock)", err, dir)
		}
		return err
	}
	defer lock.Release()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	s := supervisor.New(herdr.New(), log.New(os.Stderr, "", log.LstdFlags))
	s.Poll = *poll
	s.Log.Printf("supervising (state in %s)", dir)
	return s.Run(ctx)
}

// up makes sure a supervisor is running, starting one in a background pane
// split from the caller's pane so it stays visible without stealing focus.
func up(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: fleet up")
	}
	dir, err := ledger.Dir()
	if err != nil {
		return err
	}
	running, err := supervisor.Running(dir)
	if err != nil {
		return err
	}
	if running {
		fmt.Printf("supervisor already running (pid %d)\n", supervisor.ReadPID(dir))
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	c := herdr.New()
	pane, err := c.PaneSplit(herdr.PaneSplitParams{
		TargetPaneID: os.Getenv("HERDR_PANE_ID"), Direction: "down", CWD: cwd, Focus: false,
	})
	if err != nil {
		return fmt.Errorf("split pane: %w", err)
	}
	// The new pane's shell has herdr's environment, not ours: carry FLEET_HOME.
	cmd := shellQuote(exe) + " supervise"
	if h := os.Getenv("FLEET_HOME"); h != "" {
		cmd = "FLEET_HOME=" + shellQuote(h) + " " + cmd
	}
	if err := c.PaneRun(pane.PaneID, cmd); err != nil {
		return fmt.Errorf("start supervisor in pane %s: %w", pane.PaneID, err)
	}
	fmt.Println(pane.PaneID)
	return nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
