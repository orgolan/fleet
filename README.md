# fleet

Run a crew of coding agents from one agent session, on top of
[herdr](https://herdr.dev). One session acts as the first mate: it spawns
crewmates, each in its own git worktree and herdr workspace, watches them, and
tells you when one needs you or finishes.

Inspired by [firstmate](https://github.com/kunchenguid/firstmate), but built on
herdr's socket API and event stream instead of tmux polling.

## How it differs from firstmate

- **Events, not polling.** The supervisor subscribes to herdr's per-pane
  `agent_status_changed` events. A slow reconcile pass (default 20s) is only a
  safety net.
- **herdr does the terminal work.** Workspaces, agent naming, status
  classification (`idle`, `working`, `blocked`, `done`) and toasts come from
  herdr; fleet does not scrape terminals to guess state.
- **Smaller.** A single Go binary plus one skill. No PR flow, no multi-machine
  support, and only Claude Code has been tested as a crewmate (see Limitations).
- **Never answers prompts.** Approval and trust prompts always go to the captain.

## Requirements

- herdr >= 0.9, running; you must launch the first mate from inside a herdr pane
  (`HERDR_ENV=1`).
- git.
- Go 1.27+ to build from source. `make check-go` verifies it. If Go is not on
  your PATH: `PATH=$HOME/.local/go/bin:$PATH make build`.
- Claude Code as the crewmate agent (default `--kind claude`).

## Quickstart

```bash
make build                 # bin/fleet
make install               # copies to ~/.local/bin (PREFIX=...) and links the skill
fleet doctor               # environment checklist
```

`make install-skill` alone symlinks `skills/fleet` to `~/.claude/skills/fleet`
(it refuses to overwrite anything that is not a symlink). Then, from a Claude
Code session inside herdr, ask it to run a crew, or drive it by hand:

```bash
fleet spawn --repo ~/projects/app fix-login "Fix the login redirect bug. Run go test ./... before finishing."
fleet up                   # start the supervisor in a background herdr pane
fleet status               # tasks merged with live herdr status
fleet read fix-login       # tail of its output
fleet send fix-login "Also add a regression test."
fleet stop fix-login       # after its branch fleet/fix-login is merged or approved
```

## Command reference

| Command | Purpose |
|---|---|
| `fleet ping` | Check the herdr socket answers. |
| `fleet events` | Stream herdr events (debugging). |
| `fleet tasks` | List recorded tasks from the ledger. |
| `fleet spawn [--kind claude] [--repo PATH] [--branch B] [--base REF] [--trust-repository] <name> [brief...] [-- agent-args...]` | Create worktree (branch `fleet/<name>`), workspace and named agent; record the task; send the brief once the agent is ready. |
| `fleet status [--json]` | Tasks merged with live herdr status. |
| `fleet send <name> <text...>` | Prompt a crewmate. Refuses if it is blocked. |
| `fleet read <name> [--lines N]` | Tail of a crewmate's output. |
| `fleet keys <name> <key...>` | Captain answers a blocked prompt, e.g. `enter`, `esc`, `ctrl+c`. |
| `fleet focus <name>` | Focus the crewmate in the herdr UI. |
| `fleet stop <name> [--force]` | Remove worktree and workspace, mark the task stopped. |
| `fleet up` | Ensure the supervisor runs (starts `fleet supervise` in a background herdr pane). |
| `fleet supervise [--poll 20s]` | Event-driven supervisor; single instance. |
| `fleet doctor` | Environment checklist. |

Agent names must match `[a-z][a-z0-9_-]{0,31}` (herdr's rule).

## How the supervisor works

`fleet supervise` (normally started by `fleet up`) holds a lock so only one runs.
It subscribes to herdr's pane lifecycle events (`pane.agent_detected`,
`pane.exited`, `pane.closed`) and, for each crewmate's pane, to
`pane.agent_status_changed`. It reacts to transitions only; repeated statuses
are ignored.

| Transition | Action |
|---|---|
| agent becomes `blocked` | Toast "needs you" with the last lines of output. |
| becomes `idle`/`done` and the brief is unsent | Deliver the brief, once. |
| `working` -> `idle`/`done` | Toast "finished a turn" with the tail of output. |
| pane exits or closes | Record `exited`, toast. |

Every state change is written to the task ledger. Roughly every `--poll` it also
reconciles ledger against `herdr agent list` to catch missed events, and it
reconnects if the event stream drops.

### Trust prompt

A fresh Claude Code in a new worktree asks "trust this folder". The agent goes
`blocked`, the brief is recorded but not sent, and the supervisor toasts you.
Resolve it in the herdr UI (`fleet focus <name>`) or, only if you approve, with
`fleet keys <name> enter` on "Yes, I trust this folder". When the agent next goes
idle the supervisor delivers the brief automatically. `--trust-repository` on
`spawn` is an explicit opt-in you choose; fleet never decides it for you.

## State and ledger

State lives in `$FLEET_HOME` (default `~/.local/state/fleet`):

```
tasks/<name>.json     one record per crewmate: repo, branch, worktree, workspace,
                      pane, brief, brief_sent, last state, timestamps
supervisor.lock/pid   single-instance lock and pid of the running supervisor
```

Worktrees are created under `~/.herdr/worktrees` on branch `fleet/<name>`.

## Safety model

- Fleet never auto-answers an approval, trust, or permission prompt. `fleet send`
  refuses while blocked; `fleet keys` exists for the captain's decision.
- It only stops or removes things it created (recorded in the ledger).
- No permission-bypass flags are added to crewmates; pass any yourself after `--`.
- Fleet does not run destructive git on your projects; it adds a worktree and
  branch, and `stop` removes only that worktree.
- The bundled skill tells the first-mate session to ask before sending keys.

## Development

```bash
make test                  # go vet + go test -race ./...
```

Tests run without herdr against `internal/herdr/fake`, an in-process fake of the
herdr socket that can emit events and hold agents, prompts, and notifications.
See [docs/architecture.md](docs/architecture.md).

## Limitations and roadmap

- Only tested with Claude Code as the crewmate; other `--kind` values are
  untested.
- The trust prompt needs the captain; there is no unattended mode.
- No PR flow: fleet does not push, open PRs, or merge. You or the first mate
  merge `fleet/<name>` branches yourself.
- Single machine only (herdr `--machine` forwarding is not used).
- One supervisor per `$FLEET_HOME`; no cross-session coordination beyond the ledger.
- Reads of crewmate output are tails, not full transcripts.
