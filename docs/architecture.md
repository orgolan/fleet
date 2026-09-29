# Architecture

## Components

- **internal/herdr**: JSON-over-socket client. Request/response calls (agents,
  prompt, read, notify, workspace/worktree creation) and `Subscribe`, which
  opens a dedicated connection for an event subscription.
- **internal/herdr/fake**: in-process fake herdr server used by tests. It holds
  agents and their statuses, records prompts and notifications, and lets a test
  `Emit` events, so supervisor behavior is tested without a real herdr.
- **internal/ledger**: one JSON file per task under `$FLEET_HOME/tasks`, atomic
  writes (temp file + rename), plus per-task locking.
- **internal/crew**: spawn, brief delivery, and the other crewmate operations.
  Delivery refuses to prompt a blocked agent.
- **internal/supervisor**: the event loop.
- **cmd/fleet**: the CLI.

## Event flow

```
 herdr server
   |  pane.agent_detected / pane.exited / pane.closed   (one meta subscription)
   |  pane.agent_status_changed {pane_id=X}             (one subscription per crew pane)
   v
 supervisor.Run  (single goroutine owns all state)
   |-- status chan --> onStatus --> handle(task, status)
   |-- meta chan   --> onMeta   --> watch/unwatch pane, mark exited
   '-- poll tick   --> reconcile (agent list vs ledger; safety net)
                         |
        handle: blocked -> toast + ledger state
                idle/done + brief unsent -> crew.Deliver -> ledger brief_sent
                working -> idle/done -> toast "finished a turn"
```

Every toast is also queued as a prompt for the first mate that spawned the task
(`mate` in the ledger, from `HERDR_PANE_ID` at spawn). The queue is flushed when
that pane is idle or done, on each alert and each poll, so the first mate hears
about finished, blocked and exited crewmates without polling. Tasks spawned
before this field existed have no mate and only toast.

## Audit of leftover workspaces

At start and every `AuditEvery` polls (15, about five minutes) the supervisor
lists herdr's workspaces and looks for ones fleet no longer accounts for: an extra
workspace labelled `fleet-supervisor` (this supervisor knows its own from
`HERDR_WORKSPACE_ID`), or the workspace of a task the ledger says is stopped or
exited. A workspace must look orphaned in two audits in a row, so one caught
mid-`fleet stop` is not reported. Each is reported once, by toast and to every
first mate the ledger knows; fleet never closes them itself. Workspaces that
match neither rule (the captain's own) are left alone.

## Stopping a crewmate

`crew.Stop` saves the crewmate's output, runs `wp-env stop` in its worktree when
it has a `.wp-env.json` (best effort, so its containers do not outlive it), marks
it stopped and removes the worktree. If git no longer knows the worktree (removed
by hand, or a removal that failed part way) there is nothing to protect, so it
closes the workspace instead and warns. Any other removal failure restores the
previous state and shows herdr's error.

## Why per-pane status subscriptions

A single global status subscription would deliver every agent's changes in
herdr, including ones fleet does not own, and the supervisor would filter them.
Subscribing per crew pane keeps traffic proportional to the crew, and a watcher
is created when `pane.agent_detected` or reconcile finds a task's pane and
cancelled when the pane exits. Each watcher resubscribes after a stream error.

## Locking

Two levels, both via files under `$FLEET_HOME`:

- **Action lock** (per task, held for the duration of a multi-step operation
  such as brief delivery or stop). If it is taken, callers get `ErrLocked`. The
  supervisor treats that as "the spawner is mid-delivery" and looks again on the
  next event or poll instead of blocking.
- **Short record lock** (held only around a read-modify-write of a task file),
  so concurrent state updates from the CLI and supervisor do not lose fields.

The supervisor itself is a single instance guarded by `supervisor.lock` with its
pid in `supervisor.pid`.

## Failure and recovery

- **Event stream drops**: meta and per-pane streams reconnect after 2s; the poll
  reconcile re-reads live state, so a missed event is corrected within `--poll`.
- **Supervisor crash or restart**: all durable state is in the ledger. On start
  it reconciles: blocked agents are re-toasted, unsent briefs are delivered when
  the agent is ready. Repeated statuses are idempotent, so duplicates do not
  resend briefs.
- **Delivery fails**: not marked sent; the next pass retries. If the agent is
  blocked, delivery is refused and the captain is toasted.
- **Pane vanishes**: recorded as `exited` (only after the task was seen live,
  to avoid marking a still-starting agent).
- **Notification failure**: logged; never fatal.
- Fleet never resolves a blocked prompt on its own.
