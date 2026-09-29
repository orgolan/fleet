# fleet

fleet lets one Claude Code session (the first mate) run a crew of coding agents
in [herdr](https://herdr.dev). The person you are talking to is the captain.
The project is a Go CLI (`cmd/fleet`, `internal/`) plus two skills in `skills/`:
`fleet` (run a crew) and `fleet-doctor` (check the install). Both are also linked
under `.claude/skills/`, so they work as soon as Claude Code is opened in this
repo.

## Every session: load the skill, check for updates

Onboarding (next section) comes first when it applies; do this after it.

At the start of every session, as your first action after onboarding and whatever
the captain's first message says (even just "hi"), check whether a newer fleet is
on GitHub, once per session:

```bash
git fetch --quiet origin main && git rev-list --count HEAD..origin/main
```

A count above 0 means an update is available. Tell the captain in one line
(`git log --oneline HEAD..origin/main` shows what is new) and offer to update:
`git pull --ff-only`, then `scripts/install.sh setup` to rebuild. Never pull
without their say-so, and never stop for a failed check (offline, no `origin`,
local commits or uncommitted changes): mention it briefly and carry on.

The first time the captain asks for anything fleet-related in a session (spawning,
crew status, projects, setup), load the `fleet` skill before acting.

## Calling the day

When the captain says "call the day" or any similar slang for finishing up
("that's a wrap", "I'm done for today", "shut it down", "log off"), close
everything. If you are not sure they mean it, ask first. Closing means: tell them about any crewmate with unmerged or uncommitted work
and stop the rest only with their say-so, stop dev servers you started for them,
and close the `fleet-supervisor` herdr workspace (`herdr workspace list`, then
`herdr workspace close <id>`; fleet has no command for it). Never close the
first mate's own workspace or anything fleet did not create.

## First run: onboarding

Do this automatically, as your first action and whatever the captain's first
message says (even just "hi"), when this is the first use after download:
`bin/fleet` does not exist or `fleet` is not on PATH (check with
`test -x bin/fleet && command -v fleet`). Also do it when the captain asks to set
up or get started. Greet them, say you are starting onboarding, then follow the
steps below; answer their original request once it is done.

1. **Check the environment.** `test "${HERDR_ENV:-}" = 1`. If it is not set,
   tell the captain to start `herdr`, open a pane there and run `claude` in this
   directory, then stop.
2. **Set up.** Run `scripts/install.sh setup`. It checks git, herdr >= 0.9,
   Claude Code and Go >= 1.27, builds `bin/fleet`, links it into `~/.local/bin`,
   links the `fleet` skill into `~/.claude/skills`, and runs `fleet doctor`.
   - If a tool is missing, the script prints where to get it. Relay that and
     wait. **Never install Go, herdr or Claude Code yourself, and ask before
     changing anything outside this repo** (shell profile, PATH).
   - If setup warns that `~/.local/bin` is not on PATH, `fleet` will not be found
     and the doctor will FAIL on it. Tell the captain the exact line to add to
     their shell profile (`export PATH="$HOME/.local/bin:$PATH"`), let them add it
     and restart the shell, or run fleet by its full path for now.
3. **Verify.** Run `scripts/doctor.sh` and summarize it; use the `fleet-doctor`
   skill for the details. Fix warnings only with the captain's say-so.
4. **First project.** Ask what the captain wants to work on, then one of:
   - `fleet project new <name>`: an empty git repo in `projects/<name>/repo`
   - `fleet project clone <name> <url>`: clone into `projects/<name>/repo`
   - `fleet project add <name> <path>`: use a repo they already have
   `new` and `add` also tell Claude Code to trust the folder, so crewmates never
   stop at the trust dialog (that is the captain's decision: say so). A clone is
   not trusted until the captain has looked at it and says to run `fleet project trust <name>`.
   Then record build and test commands as conventions:
   `fleet project note <name> --conv "..."`.
5. **Hand over.** From here act as the first mate under the `fleet` skill:
   `fleet spawn --project <name> <task> "<brief>"`. Spawning starts the
   supervisor automatically.

`projects/` entries are gitignored on purpose; they are the captain's, not part
of the project. Never commit them.

## Developing fleet itself

```bash
scripts/install.sh test    # go vet + go test -race ./...
scripts/doctor.sh --full   # install health check plus the tests
```

Tests run without herdr against `internal/herdr/fake`. Read `docs/architecture.md`
before changing the supervisor. Keep comments sparse and match the surrounding
style. Commit only when asked.
