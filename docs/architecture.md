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
