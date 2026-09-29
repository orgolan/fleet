# fleet

Run a crew of coding agents from one agent session, on top of
[herdr](https://herdr.dev). One session acts as the first mate: it spawns
crewmates, each in its own git worktree and herdr workspace, watches them, and
tells you when one needs you or finishes. Fleet talks to herdr's socket API and
event stream, so there is no polling loop.

## Requirements

- herdr >= 0.9, running; launch the first mate from inside a herdr pane (`HERDR_ENV=1`).
- git.
- Go 1.27+ to build. `scripts/install.sh check-go` verifies it (uses `$GO`, else
  the first new-enough `go` on PATH, `$HOME/.local/go/bin/go` or `/usr/local/go/bin/go`).
- Claude Code as the crewmate agent (default `--kind claude`).

## Quickstart

1. Install [herdr](https://herdr.dev), [Claude Code](https://claude.com/claude-code) and Go.
2. Clone, start herdr, and open Claude Code in the clone:

   ```bash
   git clone https://github.com/orgolan/fleet && cd fleet
   herdr            # then, in a herdr pane:
   claude
   ```

3. Say "set me up". Claude follows `CLAUDE.md`: it checks your tools, builds and
   installs fleet, verifies it, and helps you create your first project.

Or do it yourself:

```bash
scripts/install.sh setup          # check tools, build, link binary + skill, run doctor
fleet project new myapp           # empty repo under projects/myapp/repo
fleet project clone myapp <url>   # ...or clone one (or: fleet project add myapp ~/code/app)
fleet spawn --project myapp fix-login "Fix the login redirect bug. Run go test ./... before finishing."
fleet status                      # tasks merged with live herdr status
fleet read fix-login              # tail of its output
fleet send fix-login "Also add a regression test."
fleet merge fix-login             # merge fleet/fix-login into its base, then stop it
scripts/doctor.sh                 # install health check (--full also runs the tests)
```

`spawn` starts the supervisor if it is not running (`fleet up` does the same by
hand). `scripts/install.sh install` links `bin/fleet` into `~/.local/bin`
(`PREFIX=...`) and the skill into `~/.claude/skills`; `uninstall` removes both.
The skill is also linked at `.claude/skills/`, so it works inside this repo
without installing. `make` targets are optional wrappers.

## Commands

| Command | Purpose |
|---|---|
| `fleet spawn [--kind claude] [--project NAME \| --repo PATH] [--branch B] [--base REF] [--with-dirty] [--one-shot] [--keep] [--trust-repository] <name> [brief...] [-- agent-args...]` | Create a worktree (branch `fleet/<name>`), workspace and named agent; record the task; send the brief once the agent is ready. |
| `fleet status [--json]` | Tasks merged with live herdr status. |
| `fleet send <name> <text...>` | Prompt a crewmate. Refuses if it is blocked. |
| `fleet read <name> [--lines N]` | Tail of a crewmate's output. |
| `fleet result <name> [--lines N]` | A crewmate's report, live or as saved at stop. |
| `fleet keys <name> <key...>` | Captain answers a blocked prompt, e.g. `enter`, `esc`, `ctrl+c`. |
| `fleet focus <name>` | Focus the crewmate in the herdr UI. |
| `fleet wait [--all] [--timeout 30m] [name...]` | Block until the named (or all live) crewmates settle. |
| `fleet merge <name> [--test "cmd"] [--keep-running]` | Merge a finished branch into its base in the project checkout (committed only if the optional test passes; conflicts abort cleanly), warn about large added files, then stop the crewmate. |
| `fleet resume <name>... \| --all` | Relaunch crewmates whose panes are gone, in their worktrees, continuing the last session. |
| `fleet stop <name> [--force]` | Remove worktree and workspace, mark the task stopped; output is saved for `fleet result`. |
| `fleet clean [--dry-run]` | Close workspaces nobody uses (stopped or exited tasks, herdr's leftover workspace on a repo's main checkout). Worktrees and records are untouched. |
| `fleet prune [--older-than 7d] [--dry-run] [name...]` | Delete records of tasks stopped longer ago than that. Live and exited tasks are never pruned. |
| `fleet project new\|clone\|add\|trust\|list\|show\|note\|agent-args\|rm` | Manage the repos in scope and their notes (see Projects). |
| `fleet up` / `fleet supervise [--poll 20s]` | Ensure the supervisor runs / run it in the foreground (single instance). |
| `fleet tasks`, `fleet ping`, `fleet events`, `fleet doctor`, `fleet version` | Ledger list, socket check, event stream (debugging), environment checklist, version. |

Agent names match `[a-z][a-z0-9_-]{0,31}` (herdr's rule).

## Projects

`projects/<name>/project.json` (repo path, default base ref) and
`projects/<name>/notes.md` record which repos are in scope. `new` and `clone`
put the repo at `projects/<name>/repo`; `add` registers one that lives
elsewhere. `rm` refuses to delete a repo under `projects/` without `--force`.
Only `projects/README.md` and `projects/_example/` are tracked; real entries are
gitignored. The folder is `$FLEET_PROJECTS`, else `projects/` in the checkout
the binary runs from, else `$FLEET_HOME/projects` (`fleet doctor` prints it).

`notes.md` is the first mate's memory of a project, as numbered one-line entries:

- **Conventions**: stable facts (build and test commands, rules). Always sent to crewmates.
- **Log**: dated lessons and outcomes. Only the newest 10 are sent.

```bash
fleet project note myapp --conv "run go test ./... before finishing"
fleet project note myapp "login redirect loops when the cookie is missing"   # log entry
fleet project note myapp --edit 2 "..."     # replace note 2 (keeps its date)
fleet project note myapp --rm 3
fleet project show myapp                    # record, notes, and the project's tasks
```

`fleet spawn --project <name>` uses the registered repo and base and appends the
conventions and recent log to the brief. Notes over 3000 characters trigger a
warning to trim them. `fleet project agent-args <name> [args...|--clear]` sets
default native agent arguments for every spawn in the project.

## Trust prompt

A fresh Claude Code in a new worktree asks "trust this folder". Registering a
project is your decision to trust it, so fleet records that up front:

- `project new` and `project add` mark the repo trusted (`--no-trust` opts out).
  `project clone` does not, because it is someone else's code: look at it, then
  run `fleet project trust <name>` (or clone with `--trust`).
- `spawn --project` marks each new worktree trusted when its project is.
- The only thing written is `projects[<path>].hasTrustDialogAccepted` in
  `~/.claude.json` (or `$CLAUDE_CONFIG_DIR/.claude.json`); the rest of the file
  is left byte-for-byte as it was.

A spawn with `--repo` (not a registered project) still stops at the prompt: the
agent goes `blocked`, the brief is held, and you get a toast. Resolve it in the
herdr UI (`fleet focus <name>`) or, only if you approve, with
`fleet keys <name> down enter` (the highlight starts on "No, exit"). Once the
agent is idle the brief is delivered. `--trust-repository` on `spawn` is git's
per-request trust for the worktree, a separate thing.

## The supervisor

`fleet supervise` (normally started by `fleet up`, in its own `fleet-supervisor`
workspace) holds a lock so only one runs. It subscribes to herdr's pane
lifecycle events and each crewmate's `pane.agent_status_changed`, and reacts to
transitions only:

| Transition | Action |
|---|---|
| agent becomes `blocked` | Toast "needs you" with the last lines of output. |
| becomes `idle`/`done` and the brief is unsent | Deliver the brief, once. |
| `working` -> `idle`/`done` | Toast "finished a turn" with the tail of output. |
| pane exits or closes | Record `exited`, toast. |

Every change is written to the task ledger. Every `--poll` it also reconciles
the ledger against `herdr agent list` to catch missed events, and it reconnects
if the stream drops. See [docs/architecture.md](docs/architecture.md).

**Automatic disposal.** An idle agent may just be waiting for you, so a
crewmate is disposed of only when it is idle, its worktree is clean, and either
its branch has its own commits and is now contained in its base (merge
`fleet/<name>` and it goes away within about 20 seconds), or it was spawned
`--one-shot` and its first turn ended. Disposal is `fleet stop` without
`--force`: the report is saved, the worktree and workspace are removed, and you
get a toast. Uncommitted or unmerged work is never removed; the toast says why.
`--keep` opts a crewmate out; crewmates spawned before this feature have no
recorded base and need `fleet stop`.

**Uncommitted work.** A worktree is a fresh checkout without your uncommitted
changes. `spawn` warns when the repo is dirty; `--with-dirty` copies the edits
and untracked files into the worktree (the repo itself is never modified).

**Briefs that never arrive.** Right after a trust prompt clears, herdr reports
idle while the UI is still starting, and text sent then can be dropped. The
supervisor waits a few seconds first and checks 25 seconds after delivery that
the agent has the brief. If not, it toasts and does not resend (a duplicate is
harder to undo than a missing brief): use `fleet read`, then `fleet send`.

## State

State lives in `$FLEET_HOME` (default `~/.local/state/fleet`):

```
tasks/<name>.json         one record per crewmate: repo, branch, worktree, workspace,
                          pane, brief, brief_sent, last state, timestamps
tasks/<name>.result.txt   its last output, saved by `fleet stop`
supervisor.lock           holds the pid of the running supervisor
```

Worktrees are created under `~/.herdr/worktrees` on branch `fleet/<name>`.

## Safety model

- Fleet never auto-answers an approval or permission prompt. `fleet send`
  refuses while blocked; `fleet keys` exists for the captain's decision. Folder
  trust is the one decision made ahead of time, by registering a project.
- It only stops or removes things it created (recorded in the ledger).
- No permission-bypass flags are added to crewmates; pass any yourself after `--`.
- It never runs destructive git on your projects: it adds a worktree and branch,
  and `stop` removes only that worktree.
- The bundled skill tells the first mate to ask before sending keys.

## Persona

The bundled skill makes the first mate talk like a pirate and call you
"Captain". Say "plain talk, please" or delete the `## Persona` section of
`skills/fleet/SKILL.md` to turn it off. It never changes commands, paths or
error text.

## Development

```bash
scripts/install.sh test    # go vet + go test -race ./... (or: make test)
scripts/release.sh v0.1.0  # cross-compile linux and macOS into dist/ with checksums; build only
```

Tests run without herdr against `internal/herdr/fake`, an in-process fake of the
herdr socket.

## Limitations

- Only tested with Claude Code as the crewmate; other `--kind` values are untested.
- The trust prompt needs the captain; there is no unattended mode.
- No PR flow: fleet does not push, open PRs, or merge on its own. You or the
  first mate merge `fleet/<name>` branches (`fleet merge`).
- Single machine only; herdr `--machine` forwarding is not used.
- One supervisor per `$FLEET_HOME`.
- Reads of crewmate output are tails, not full transcripts.
