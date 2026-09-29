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
- Go 1.27+ to build from source. `scripts/install.sh check-go` verifies it. It
  uses `$GO`, else the first Go that is new enough among `go` on PATH,
  `$HOME/.local/go/bin/go` and `/usr/local/go/bin/go`.
- Claude Code as the crewmate agent (default `--kind claude`).

## Quickstart

1. Install [herdr](https://herdr.dev) (>= 0.9), [Claude Code](https://claude.com/claude-code)
   and Go >= 1.27. Git is assumed.
2. Clone, start herdr, and open Claude Code in the clone:

   ```bash
   git clone https://github.com/orgolan/fleet && cd fleet
   herdr            # then, in a herdr pane:
   claude
   ```

3. Say "set me up". Claude reads `CLAUDE.md` and runs the onboarding: it checks
   your tools, builds and installs fleet, verifies it, and helps you create your
   first project. Prefer to do it yourself?

```bash
scripts/install.sh setup          # check tools, build, link binary + skill, run doctor
fleet project new myapp           # empty repo under projects/myapp/repo
fleet project clone myapp <url>   # ...or clone one (or: fleet project add myapp ~/code/app)
fleet spawn --project myapp fix-login "Fix the login redirect bug. Run go test ./... before finishing."
fleet status                      # tasks merged with live herdr status
fleet read fix-login              # tail of its output
fleet send fix-login "Also add a regression test."
fleet stop fix-login              # after its branch fleet/fix-login is merged or approved
scripts/doctor.sh                 # is the install healthy? (--full also runs the tests)
```

`spawn` starts the supervisor if it is not running (`fleet up` does the same by
hand). `make` targets (`make setup`, `make doctor`, ...) are optional wrappers.
`scripts/install.sh install` links `bin/fleet` into `~/.local/bin` (`PREFIX=...`)
and the skill into `~/.claude/skills`; `uninstall` removes both links. The skill
is also linked at `.claude/skills/`, so it works inside this repo with no install.

## Command reference

| Command | Purpose |
|---|---|
| `fleet ping` | Check the herdr socket answers. |
| `fleet events` | Stream herdr events (debugging). |
| `fleet tasks` | List recorded tasks from the ledger. |
| `fleet spawn [--kind claude] [--project NAME \| --repo PATH] [--branch B] [--base REF] [--trust-repository] <name> [brief...] [-- agent-args...]` | Create worktree (branch `fleet/<name>`), workspace and named agent; record the task; send the brief once the agent is ready. |
| `fleet project new\|clone\|add\|list\|show\|note\|rm` | Create, clone or register the repos in scope, with per-project notes (see Projects). |
| `fleet status [--json]` | Tasks merged with live herdr status. |
| `fleet send <name> <text...>` | Prompt a crewmate. Refuses if it is blocked. |
| `fleet read <name> [--lines N]` | Tail of a crewmate's output. |
| `fleet keys <name> <key...>` | Captain answers a blocked prompt, e.g. `enter`, `esc`, `ctrl+c`. |
| `fleet focus <name>` | Focus the crewmate in the herdr UI. |
| `fleet stop <name> [--force]` | Remove worktree and workspace, mark the task stopped. |
| `fleet up` | Ensure the supervisor runs (starts `fleet supervise` in its own `fleet-supervisor` herdr workspace). `spawn` does this automatically. |
| `fleet supervise [--poll 20s]` | Event-driven supervisor; single instance. |
| `fleet doctor` | Environment checklist. |

Agent names must match `[a-z][a-z0-9_-]{0,31}` (herdr's rule).

## Projects

`projects/<name>/project.json` (repo path, default base ref) and
`projects/<name>/notes.md` (conventions, test commands, gotchas) record which
repos are in scope. `fleet project new` and `clone` also put the repo itself at
`projects/<name>/repo`; `add` registers a repo that lives elsewhere. `rm` refuses
to delete a repo that lives in `projects/` unless you pass `--force`.
`fleet spawn --project <name>` uses the registered repo and base and appends the
notes to the crewmate's brief, so the first mate and its crew share context.

Only `projects/README.md` and `projects/_example/` are tracked; real entries are
gitignored so your repo list stays local. The folder is `$FLEET_PROJECTS`, else
`projects/` in the checkout the binary runs from (`bin/fleet`), else
`$FLEET_HOME/projects`. `install` links the binary instead of copying it, so
it always finds the checkout. `fleet doctor` prints the folder in use.

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
supervisor.lock       single-instance lock; holds the pid of the running supervisor
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
scripts/install.sh test    # go vet + go test -race ./... (or: make test)
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
