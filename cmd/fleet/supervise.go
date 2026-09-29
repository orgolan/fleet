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

// supervisorLabel names the herdr workspace the supervisor lives in.
const supervisorLabel = "fleet-supervisor"

// up makes sure a supervisor is running.
func up(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: fleet up")
	}
	started, pane, pid, err := ensureSupervisor()
	switch {
	case err != nil:
		return err
	case started:
		fmt.Println(pane)
	default:
		fmt.Printf("supervisor already running (pid %d)\n", pid)
	}
	return nil
}

// ensureSupervisor starts a supervisor if none is running, in a workspace of its
// own (not a split of the caller's pane, so closing the caller's pane or tab
// cannot kill it). The workspace is not focused.
func ensureSupervisor() (started bool, pane string, pid int, err error) {
	dir, err := ledger.Dir()
	if err != nil {
		return false, "", 0, err
	}
	running, err := supervisor.Running(dir)
	if err != nil {
		return false, "", 0, err
	}
	if running {
		return false, "", supervisor.ReadPID(dir), nil
	}
	exe, err := os.Executable()
	if err != nil {
		return false, "", 0, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return false, "", 0, err
	}
	c := herdr.New()
	ws, err := c.WorkspaceCreate(cwd, supervisorLabel)
	if err != nil {
		return false, "", 0, fmt.Errorf("create supervisor workspace: %w", err)
	}
	// The new pane's shell has herdr's environment, not ours: carry FLEET_HOME.
	cmd := shellQuote(exe) + " supervise"
	if h := os.Getenv("FLEET_HOME"); h != "" {
		cmd = "FLEET_HOME=" + shellQuote(h) + " " + cmd
	}
	if err := c.PaneRun(ws.RootPane.PaneID, cmd); err != nil {
		return false, "", 0, fmt.Errorf("start supervisor in pane %s: %w", ws.RootPane.PaneID, err)
	}
	return true, ws.RootPane.PaneID, 0, nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
