# fleet

Run a crew of coding agents from one agent session, on top of
[herdr](https://herdr.dev). One session acts as the first mate: it spawns
crewmates, each in its own git worktree and herdr workspace, watches them, and
tells you when one needs you or finishes.

Inspired by [firstmate](https://github.com/kunchenguid/firstmate), but built on
herdr's socket API and event stream instead of tmux polling.

## Fleet vs firstmate

[firstmate](https://github.com/kunchenguid/firstmate) is a much larger "agent
distro": several session backends, many primary harnesses, PR and merge modes,
secondmates, and more. Fleet is a small tool that does one thing well on herdr.
Pick firstmate if you need what fleet lacks.

| | fleet | firstmate |
|---|---|---|
| Backend | herdr only, over its socket API | tmux (default), herdr, zellij, cmux, Orca |
| Supervision | herdr event stream; a slow reconcile is only a safety net | a watcher script with per-harness turn-end guards |
| Agents | Claude Code tested as crewmate; other herdr kinds untested | several harnesses verified as the primary session |
| Shipping work | none: you merge `fleet/<name>` branches yourself | per-project modes: PR, `no-mistakes`, local-only, Gerrit |
| Machines | one | local plus SSH secondmates |
| Install | one Go binary plus two skills; `CLAUDE.md` walks you through setup | clone the repo; the repo is the distro |
| Approval prompts | never answered by fleet | first mate escalates real decisions |
| Size | small Go codebase (see `cmd/` and `internal/`) | a full harness of scripts, skills and policies |

What fleet adds: events instead of polling, herdr does the terminal work
(workspaces, status classification, toasts), and a project registry with notes
the first mate reads before briefing.

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
| `fleet spawn [--kind claude] [--project NAME \| --repo PATH] [--branch B] [--base REF] [--with-dirty] [--one-shot] [--keep] [--trust-repository] <name> [brief...] [-- agent-args...]` | Create worktree (branch `fleet/<name>`), workspace and named agent; record the task; send the brief once the agent is ready. |
| `fleet project new\|clone\|add\|trust\|list\|show\|note\|rm` | Create, clone or register the repos in scope, and keep notes on them (see Projects). |
| `fleet status [--json]` | Tasks merged with live herdr status. |
| `fleet send <name> <text...>` | Prompt a crewmate. Refuses if it is blocked. |
| `fleet read <name> [--lines N]` | Tail of a crewmate's output. |
| `fleet result <name> [--lines N]` | A crewmate's report, live or as saved when it was stopped. |
| `fleet keys <name> <key...>` | Captain answers a blocked prompt, e.g. `enter`, `esc`, `ctrl+c`. |
| `fleet focus <name>` | Focus the crewmate in the herdr UI. |
| `fleet stop <name> [--force]` | Remove worktree and workspace, mark the task stopped. Its last output is saved for `fleet result`. |
| `fleet prune [--older-than 7d] [--dry-run] [name...]` | Delete the records (and saved output) of tasks stopped longer ago than that. Live and exited tasks are never pruned. |
| `fleet up` | Ensure the supervisor runs (starts `fleet supervise` in its own `fleet-supervisor` herdr workspace). `spawn` does this automatically. |
| `fleet supervise [--poll 20s]` | Event-driven supervisor; single instance. |
| `fleet doctor` | Environment checklist. |
| `fleet version` | Print the version. |

Agent names must match `[a-z][a-z0-9_-]{0,31}` (herdr's rule).

## Projects

`projects/<name>/project.json` (repo path, default base ref) and
`projects/<name>/notes.md` (conventions, test commands, gotchas) record which
repos are in scope. `fleet project new` and `clone` also put the repo itself at
`projects/<name>/repo`; `add` registers a repo that lives elsewhere. `rm` refuses
to delete a repo that lives in `projects/` unless you pass `--force`.
### Notes: the first mate's memory of a project

`notes.md` has two sections of one-line entries, numbered across both:

- **Conventions**: stable facts (build and test commands, rules). Always sent to crewmates.
- **Log**: dated lessons and outcomes. Only the newest 10 are sent.

```bash
fleet project note myapp --conv "run go test ./... before finishing"
fleet project note myapp "login redirect loops when the cookie is missing"   # dated log entry
fleet project show myapp                    # record, numbered notes, and the project's tasks
fleet project note myapp --edit 2 "..."     # replace note 2 (keeps its date)
fleet project note myapp --rm 3
```

`fleet spawn --project <name>` uses the registered repo and base, records the
project on the task, and appends the conventions and recent log to the
crewmate's brief. Notes over 3000 characters trigger a warning to trim them.
`fleet stop` reminds you to record what the next crewmate should know; the
crewmate's report itself is kept (`fleet result`), and `fleet project show` lists
the project's tasks from the ledger. Old flat notes files are migrated to the
Log section the first time they are written.

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

A fresh Claude Code in a new worktree asks "trust this folder". Registering a
project is your decision to trust it, so fleet records that up front:

- `fleet project new` and `fleet project add` mark the repo trusted in Claude Code
  (`--no-trust` opts out). `fleet project clone` does not, because it is someone
  else's code: look at it, then run `fleet project trust <name>` (or clone with
  `--trust`).
- `fleet spawn --project <name>` marks each new worktree trusted when its project
  is, so crewmates start without the dialog.
- The only thing fleet writes is `projects[<path>].hasTrustDialogAccepted` in
  `~/.claude.json` (or `$CLAUDE_CONFIG_DIR/.claude.json`); the rest of that file
  is left byte-for-byte as it was.

A spawn with `--repo` (not a registered project) still stops at the prompt: the
agent goes `blocked`, the brief is recorded but not sent, and the supervisor toasts
you. Resolve it in the herdr UI (`fleet focus <name>`) or, only if you approve, with
`fleet keys <name> down enter` (the highlight starts on "No, exit"). When the agent
next goes idle the supervisor delivers the brief automatically.
`--trust-repository` on `spawn` is git's per-request trust for the worktree, a
separate thing.

## Projects

`projects/<name>/project.json` (repo path, default base ref) and
`projects/<name>/notes.md` (conventions, test commands, gotchas) record which
repos are in scope. `fleet project new` and `clone` also put the repo itself at
`projects/<name>/repo`; `add` registers a repo that lives elsewhere. `rm` refuses
to delete a repo that lives in `projects/` unless you pass `--force`.
### Notes: the first mate's memory of a project

`notes.md` has two sections of one-line entries, numbered across both:

- **Conventions**: stable facts (build and test commands, rules). Always sent to crewmates.
- **Log**: dated lessons and outcomes. Only the newest 10 are sent.

```bash
fleet project note myapp --conv "run go test ./... before finishing"
fleet project note myapp "login redirect loops when the cookie is missing"   # dated log entry
fleet project show myapp                    # record, numbered notes, and the project's tasks
fleet project note myapp --edit 2 "..."     # replace note 2 (keeps its date)
fleet project note myapp --rm 3
```

`fleet spawn --project <name>` uses the registered repo and base, records the
project on the task, and appends the conventions and recent log to the
crewmate's brief. Notes over 3000 characters trigger a warning to trim them.
`fleet stop` reminds you to record what the next crewmate should know; the
crewmate's report itself is kept (`fleet result`), and `fleet project show` lists
the project's tasks from the ledger. Old flat notes files are migrated to the
Log section the first time they are written.

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

### Finished crewmates are disposed automatically

An idle agent is not necessarily finished (it may be waiting for your answer), so
fleet disposes a crewmate in only two unambiguous cases, and only when it is idle
and its worktree is clean:

- **Its work is merged.** Its branch has commits of its own and is now contained
  in its base (`main`, or whatever it was cut from). Merge `fleet/<name>` and the
  crewmate goes away on its own, within about 20 seconds.
- **It is a one-shot task** (`fleet spawn --one-shot`, meant for reviews and
  reports) and its first turn just ended, and its output shows it really received
  the brief.

Disposal is `fleet stop` without `--force`: the report is saved (`fleet result`),
the worktree and workspace are removed, and you get a toast. If the worktree has
uncommitted changes or unmerged work, nothing is removed and the toast says why.
`--keep` on spawn opts a crewmate out. Crewmates spawned before this feature have
no recorded base and are not auto-disposed; use `fleet stop`.

### Uncommitted work

A crewmate's worktree is a fresh checkout, so it does not contain uncommitted
changes in the repo. `fleet spawn` warns when the repo is dirty;
`--with-dirty` copies the edits and untracked files into the new worktree (the
repo itself is never modified).

### Briefs that never arrive

Right after a trust prompt clears, herdr reports the agent idle while its UI is
still starting, and text sent then can be dropped. The supervisor waits a few
seconds before sending in that case, and 25 seconds after any delivery it checks
that the agent really has the brief. If not, it toasts you and does not resend
(a duplicate brief is harder to undo than a missing one): use `fleet read`, then
`fleet send`.

## State and ledger

State lives in `$FLEET_HOME` (default `~/.local/state/fleet`):

```
tasks/<name>.json     one record per crewmate (fields a newer fleet wrote are kept when an older one rewrites it): repo, branch, worktree, workspace,
                      pane, brief, brief_sent, last state, timestamps
tasks/<name>.result.txt  its last output, saved by `fleet stop`
supervisor.lock       single-instance lock; holds the pid of the running supervisor
```

Worktrees are created under `~/.herdr/worktrees` on branch `fleet/<name>`.

## Safety model

- Fleet never auto-answers an approval or permission prompt. `fleet send` refuses
  while blocked; `fleet keys` exists for the captain's decision. Folder trust is
  the one decision made ahead of time: by registering a project (see Trust prompt).
- It only stops or removes things it created (recorded in the ledger).
- No permission-bypass flags are added to crewmates; pass any yourself after `--`.
- Fleet does not run destructive git on your projects; it adds a worktree and
  branch, and `stop` removes only that worktree.
- The bundled skill tells the first-mate session to ask before sending keys.

## Releases

`scripts/release.sh v0.1.0` cross-compiles linux and macOS (amd64, arm64) into
`dist/` with checksums. It only builds; publish with `gh release create`.

## Persona

The bundled skill makes the first mate talk like a pirate first mate and call you
"Captain". Tell it to drop the act ("plain talk, please") or delete the
`## Persona` section of `skills/fleet/SKILL.md` to turn it off. It never changes
commands, paths or error text.

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
