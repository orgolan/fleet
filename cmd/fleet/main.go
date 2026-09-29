// Command fleet coordinates a crew of coding agents on top of herdr.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
)

// version is set at release time: -ldflags "-X main.version=v0.1.0".
var version = "dev"

const usage = `usage: fleet <command> [args]

commands:
  ping       check the herdr socket and print server version
  events     stream agent status changes (Ctrl+C to stop)
  spawn      start a crewmate in its own worktree: fleet spawn [flags] <name> [brief...] [-- agent-args...]
  project    manage the repos in scope and their notes: fleet project add|list|show|note|rm
  tasks      list recorded tasks
  status     tasks merged with live herdr state: fleet status [--json]
  send       prompt a crewmate (no wait): fleet send <name> <text...>
  read       tail a crewmate's recent output: fleet read <name> [--lines N]
  result     a crewmate's report, even after it was stopped: fleet result <name> [--lines N]
  keys       send keys to a crewmate, e.g. to answer a prompt: fleet keys <name> <key...>
  focus      focus a crewmate in the herdr UI: fleet focus <name>
  wait       block until crewmates settle: fleet wait [--all] [--timeout 30m] [name...]
  merge      land a finished crewmate's branch and stop it: fleet merge <name> [--test "cmd"] [--keep-running]
  resume     relaunch crewmates whose panes are gone: fleet resume <name>... | --all
  stop       remove a crewmate's worktree and mark it stopped: fleet stop <name> [--force]
  prune      delete records of old stopped tasks: fleet prune [--older-than 7d] [--dry-run] [name...]
  supervise  watch the crew: deliver briefs, notify on blocked/finished/exited
  up         ensure a supervisor is running (in a background herdr pane)
  doctor     check the environment
  version    print the fleet version
  help       print this help
`

// commands maps each subcommand to its implementation.
var commands = map[string]func(args []string) error{
	"ping":      func([]string) error { return ping() },
	"events":    func([]string) error { return events() },
	"spawn":     spawn,
	"project":   project,
	"tasks":     func([]string) error { return tasks() },
	"status":    status,
	"send":      send,
	"read":      read,
	"result":    result,
	"keys":      keys,
	"focus":     focus,
	"wait":      wait,
	"merge":     merge,
	"resume":    resume,
	"stop":      stop,
	"prune":     prune,
	"supervise": supervise,
	"up":        up,
	"doctor":    doctor,
	"version":   func([]string) error { fmt.Println("fleet", version); return nil },
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	name := os.Args[1]
	switch name {
	case "help", "-h", "--help":
		fmt.Print(usage)
		return
	}
	run, ok := commands[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "fleet: unknown command %q\n\n%s", name, usage)
		os.Exit(2)
	}
	err := run(os.Args[2:])
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
	default:
		fmt.Fprintln(os.Stderr, "fleet:", err)
		os.Exit(1)
	}
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// parseInterspersed parses flags that may appear before or after positional
// arguments (fleet read NAME --lines 5), returning the positionals.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}
