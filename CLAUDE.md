# fleet

fleet lets one Claude Code session (the first mate) run a crew of coding agents
in [herdr](https://herdr.dev). The person you are talking to is the captain.
The project is a Go CLI (`cmd/fleet`, `internal/`) plus two skills in `skills/`:
`fleet` (run a crew) and `fleet-doctor` (check the install). Both are also linked
under `.claude/skills/`, so they work as soon as Claude Code is opened in this
repo.

## First run: onboarding

Do this when the captain has just cloned the repo, asks to set up or get started,
or when `bin/fleet` does not exist or `fleet` is not on PATH.

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
