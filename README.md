# fleet

A crew of coding agents on herdr. Inspired by firstmate, but built on herdr's
socket API and event stream instead of tmux polling.

    go build -o bin/fleet ./cmd/fleet
    bin/fleet ping
    bin/fleet events
    bin/fleet spawn [--kind claude] [--repo PATH] <name> [brief...] [-- agent-args...]
    bin/fleet supervise   # deliver briefs; notify on blocked, finished, exited
    bin/fleet tasks

Layout: `internal/herdr` socket client, `internal/supervisor` event loop,
`internal/crew` spawning, `internal/ledger` task state, `skills/` fleet skills.
