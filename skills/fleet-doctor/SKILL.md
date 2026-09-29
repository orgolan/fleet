---
name: fleet-doctor
description: Check that fleet is installed correctly and in good shape, and diagnose problems. Use when the user asks to check, verify, diagnose, repair or optimize the fleet install, when fleet commands misbehave, toasts stop arriving, or after cloning and running setup.
---

# Fleet doctor

Health check for a fleet install. It only reads; it never fixes anything itself.

## Run it

```bash
scripts/doctor.sh          # from the fleet checkout; add --full to also run the tests
```

It checks tools (git, herdr >= 0.9, Claude Code, Go), the binary (built, current
with the source, on PATH and linked to this checkout), skills, `fleet doctor`,
registered projects (paths exist and are git repos, leftover `fleet/*` branches)
and housekeeping (old task records, duplicate supervisor workspaces). Output is
`ok`, `warn` or `FAIL` lines, each problem with a `fix:` line. Exit status is 1
on any FAIL.

## Then

1. Summarize: what is healthy, the FAILs first, then the warnings that matter.
2. For each problem give the exact `fix:` command from the output.
3. Ask before running any fix that changes things outside the fleet checkout
   (installing tools, editing shell profiles, deleting branches or task records).
   Rebuilding (`scripts/install.sh build`) and re-linking are safe to offer.
4. Re-run `scripts/doctor.sh` after fixes and report the result.

Never delete branches, worktrees or task records on your own. Never install Go,
herdr or Claude Code yourself; tell the captain where to get them.
