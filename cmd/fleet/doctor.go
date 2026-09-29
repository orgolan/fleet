package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"fleet/internal/herdr"
	"fleet/internal/ledger"
	"fleet/internal/projects"
	"fleet/internal/supervisor"
)

// agentCLIs are looked up on PATH for information only.
var agentCLIs = []string{"claude", "codex", "gemini", "opencode", "pi"}

func doctor(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("usage: fleet doctor")
	}
	failed := 0
	check := func(ok bool, msg string) {
		mark := "ok  "
		if !ok {
			mark = "FAIL"
			failed++
		}
		fmt.Printf("%s  %s\n", mark, msg)
	}

	if os.Getenv("HERDR_ENV") == "1" {
		check(true, "HERDR_ENV=1 (running inside herdr)")
	} else {
		check(false, "HERDR_ENV is not 1: run fleet inside a herdr pane")
	}

	var p herdr.Ping
	if err := herdr.New().Call("ping", nil, &p); err != nil {
		check(false, fmt.Sprintf("herdr socket %s: %v", herdr.SocketPath(), err))
	} else {
		check(true, fmt.Sprintf("herdr socket %s: herdr %s, protocol %d", herdr.SocketPath(), p.Version, p.Protocol))
	}

	if path, err := exec.LookPath("git"); err != nil {
		check(false, "git not found on PATH")
	} else {
		check(true, "git: "+path)
	}

	dir, err := ledger.Dir()
	if err == nil {
		err = writable(dir)
	}
	if err != nil {
		check(false, fmt.Sprintf("state dir %s not writable: %v", dir, err))
	} else {
		check(true, "state dir writable: "+dir)
	}

	if running, err := supervisor.Running(dir); err != nil {
		check(false, fmt.Sprintf("supervisor lock: %v", err))
	} else if running {
		check(true, fmt.Sprintf("supervisor running (pid %d)", supervisor.ReadPID(dir)))
	} else {
		fmt.Println("warn  supervisor not running: start it with `fleet up`")
	}

	if pd, err := projects.Dir(); err != nil {
		fmt.Printf("warn  projects dir: %v\n", err)
	} else if ps, err := projects.List(); err != nil {
		check(false, fmt.Sprintf("projects dir %s: %v", pd, err))
	} else {
		fmt.Printf("info  projects dir %s: %d registered\n", pd, len(ps))
	}

	for _, name := range agentCLIs {
		if path, err := exec.LookPath(name); err == nil {
			fmt.Printf("info  agent CLI %s: %s\n", name, path)
		} else {
			fmt.Printf("info  agent CLI %s: not on PATH\n", name)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d check(s) failed", failed)
	}
	return nil
}

// writable creates the directory if needed and proves it accepts a file.
func writable(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".doctor-*")
	if err != nil {
		return err
	}
	f.Close()
	return os.Remove(filepath.Join(dir, filepath.Base(f.Name())))
}
