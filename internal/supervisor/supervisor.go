// Package supervisor watches the crew through herdr's event stream and acts
// only when something needs attention: it delivers briefs once an agent is
// ready, tells the captain when an agent is blocked or finished, and records
// state changes in the ledger.
package supervisor

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/orgolan/fleet/internal/crew"
	"github.com/orgolan/fleet/internal/herdr"
	"github.com/orgolan/fleet/internal/ledger"
)

// Supervisor is not safe for concurrent use; Run owns all its state.
type Supervisor struct {
	C    *herdr.Client
	Log  *log.Logger
	Poll time.Duration // reconcile interval, a safety net for missed events

	// Settle is how long to wait before sending a brief to an agent that has just
	// left a blocked prompt: herdr reports it idle while its TUI is still starting
	// up, and text sent then is silently dropped. Zero disables the wait.
	Settle time.Duration
	// Verify is how long after delivery to check that the agent really took the
	// brief, and warn the captain if not. Zero disables the check.
	Verify time.Duration
	ctx    context.Context

	last      map[string]herdr.AgentStatus // task name -> last handled status
	seen      map[string]bool              // task name -> was ever observed live
	watches   map[string]context.CancelFunc
	status    chan herdr.AgentStatusChanged
	noDispose map[string]bool // tasks fleet declined to dispose (say why once, then leave them)
}

func New(c *herdr.Client, l *log.Logger) *Supervisor {
	return &Supervisor{
		C: c, Log: l, Poll: 20 * time.Second, Settle: 4 * time.Second, Verify: 25 * time.Second,
		last: map[string]herdr.AgentStatus{}, seen: map[string]bool{},
		watches: map[string]context.CancelFunc{}, noDispose: map[string]bool{},
		status: make(chan herdr.AgentStatusChanged, 64),
	}
}

// Run blocks until ctx is cancelled.
func (s *Supervisor) Run(ctx context.Context) error {
	s.ctx = ctx
	meta := s.metaEvents(ctx)
	tick := time.NewTicker(s.Poll)
	defer tick.Stop()
	s.reconcile(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-s.status:
			s.onStatus(ev.PaneID, ev.Status)
		case ev := <-meta:
			s.onMeta(ctx, ev)
		case <-tick.C:
			s.reconcile(ctx)
		}
	}
}

// metaEvents streams pane lifecycle events, reconnecting until ctx ends.
func (s *Supervisor) metaEvents(ctx context.Context) <-chan herdr.Event {
	out := make(chan herdr.Event, 64)
	go func() {
		for ctx.Err() == nil {
			evs, errc := s.C.Subscribe(ctx,
				herdr.Sub{"type": "pane.agent_detected"},
				herdr.Sub{"type": "pane.exited"},
				herdr.Sub{"type": "pane.closed"})
			for ev := range evs {
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			}
			if err := <-errc; err != nil && ctx.Err() == nil {
				s.Log.Printf("event stream: %v (reconnecting)", err)
			}
			sleep(ctx, 2*time.Second)
		}
	}()
	return out
}

// watch follows one pane's agent status until the pane goes away.
func (s *Supervisor) watch(ctx context.Context, pane string) {
	if _, ok := s.watches[pane]; ok {
		return
	}
	wctx, cancel := context.WithCancel(ctx)
	s.watches[pane] = cancel
	go func() {
		for wctx.Err() == nil {
			evs, errc := s.C.Subscribe(wctx, herdr.Sub{"type": "pane.agent_status_changed", "pane_id": pane})
			for ev := range evs {
				var d herdr.AgentStatusChanged
				if json.Unmarshal(ev.Data, &d) == nil && d.PaneID == pane {
					select {
					case s.status <- d:
					case <-wctx.Done():
						return
					}
				}
			}
			<-errc
			sleep(wctx, 2*time.Second)
		}
	}()
}

func (s *Supervisor) unwatch(pane string) {
	if cancel, ok := s.watches[pane]; ok {
		cancel()
		delete(s.watches, pane)
	}
}

func (s *Supervisor) taskByPane(pane string) (ledger.Task, bool) {
	ts, err := ledger.List()
	if err != nil {
		s.Log.Printf("ledger: %v", err)
		return ledger.Task{}, false
	}
	for _, t := range ts {
		if t.PaneID == pane && !ended(t) {
			return t, true
		}
	}
	return ledger.Task{}, false
}

// ended reports whether a task is finished for good: its pane is gone or the
// captain stopped it, so it must not be watched, re-notified or resurrected.
func ended(t ledger.Task) bool { return t.State == "exited" || t.State == "stopped" }

// reconcile aligns watchers and state with what herdr and the ledger say now.
func (s *Supervisor) reconcile(ctx context.Context) {
	agents, err := s.C.Agents()
	if err != nil {
		s.Log.Printf("agent list: %v", err)
		return
	}
	live := map[string]herdr.Agent{}
	for _, a := range agents {
		live[a.PaneID] = a
	}
	ts, err := ledger.List()
	if err != nil {
		s.Log.Printf("ledger: %v", err)
		return
	}
	for _, t := range ts {
		if ended(t) {
			continue
		}
		if a, ok := live[t.PaneID]; ok {
			s.seen[t.Name] = true
			s.watch(ctx, t.PaneID)
			s.handle(t, a.Status)
			s.maybeDispose(t, a.Status, false)
		} else if s.seen[t.Name] {
			s.exited(t)
		}
	}
}

func (s *Supervisor) onMeta(ctx context.Context, ev herdr.Event) {
	var d struct {
		PaneID string `json:"pane_id"`
	}
	if json.Unmarshal(ev.Data, &d) != nil || d.PaneID == "" {
		return
	}
	switch ev.Kind {
	case "pane.agent_detected":
		if _, ok := s.taskByPane(d.PaneID); ok {
			s.watch(ctx, d.PaneID)
			s.reconcile(ctx) // pick up the initial status
		}
	case "pane.exited", "pane.closed":
		if t, ok := s.taskByPane(d.PaneID); ok {
			s.exited(t)
		}
	}
}

func (s *Supervisor) onStatus(pane string, st herdr.AgentStatus) {
	if t, ok := s.taskByPane(pane); ok {
		s.seen[t.Name] = true
		s.handle(t, st)
	}
}

func (s *Supervisor) exited(t ledger.Task) {
	s.unwatch(t.PaneID)
	delete(s.last, t.Name)
	// The caller's copy may predate `fleet stop`, which marks the task stopped
	// before removing its worktree (closing the pane): that is not an exit.
	if cur, err := ledger.Load(t.Name); err == nil && ended(cur) {
		return
	}
	s.setState(t.Name, "exited")
	s.Log.Printf("%s: pane %s exited", t.Name, t.PaneID)
	s.notify("fleet: "+t.Name+" exited", "The pane closed or the agent process ended.", "request")
}

// handle reacts to a status transition; repeated statuses are ignored.
func (s *Supervisor) handle(t ledger.Task, st herdr.AgentStatus) {
	prev := s.last[t.Name]
	if st == prev {
		return
	}
	s.last[t.Name] = st
	s.Log.Printf("%s: %s -> %s", t.Name, orDash(string(prev)), st)
	switch st {
	case herdr.Blocked:
		s.setState(t.Name, "blocked")
		s.notify("fleet: "+t.Name+" needs you", s.tail(t.Name), "request")
	case herdr.Idle, herdr.Done:
		if !t.BriefSent && t.Brief != "" {
			s.deliver(t, prev == herdr.Blocked)
			return
		}
		s.setState(t.Name, string(st))
		if prev == herdr.Working {
			if !s.maybeDispose(t, st, true) {
				s.notify("fleet: "+t.Name+" finished a turn", s.tail(t.Name), "done")
			}
			return
		}
		s.maybeDispose(t, st, false)
	default:
		s.setState(t.Name, string(st))
	}
}

// deliver sends the brief. afterBlock is true when the agent has just left a
// blocked prompt (typically the trust dialog): wait for its TUI to settle first.
// This blocks the event loop for Settle, which is fine for a small crew.
func (s *Supervisor) deliver(t ledger.Task, afterBlock bool) {
	if afterBlock && s.Settle > 0 {
		sleep(s.ctx, s.Settle)
		if !s.stillReady(t.PaneID) {
			delete(s.last, t.Name) // changed state while settling; look again next event or pass
			return
		}
	}
	err := crew.Deliver(s.C, t.Name, false, false)
	switch {
	case err == nil:
		s.Log.Printf("%s: brief delivered", t.Name)
		s.last[t.Name] = herdr.Working
		if s.Verify > 0 {
			go s.verify(t)
		}
	case errors.Is(err, ledger.ErrLocked):
		delete(s.last, t.Name) // the spawner is mid-delivery; look again next pass
	case errors.Is(err, crew.ErrBlocked):
		s.last[t.Name] = herdr.Blocked
		s.setState(t.Name, "blocked")
		s.notify("fleet: "+t.Name+" needs you", "Blocked before its brief could be sent.\n"+s.tail(t.Name), "request")
	default:
		// Not sent; forget the status so the next pass retries.
		s.Log.Printf("%s: brief delivery failed: %v", t.Name, err)
		delete(s.last, t.Name)
	}
}

// stillReady reports whether the agent in pane is still idle or done.
func (s *Supervisor) stillReady(pane string) bool {
	agents, err := s.C.Agents()
	if err != nil {
		return false
	}
	for _, a := range agents {
		if a.PaneID == pane {
			return a.Status == herdr.Idle || a.Status == herdr.Done
		}
	}
	return false
}

// verify checks, after Verify, that a delivered brief reached the agent, and
// warns the captain if not. It never resends: a lost brief is cheap to resend by
// hand, a duplicated one is not cheap to undo. An agent still working is taken
// as proof; an idle one must show the start of the brief (or Claude Code's
// pasted-text placeholder) in its recent output.
func (s *Supervisor) verify(t ledger.Task) {
	sleep(s.ctx, s.Verify)
	if s.ctx.Err() != nil {
		return
	}
	if cur, err := ledger.Load(t.Name); err != nil || ended(cur) {
		return
	}
	if !s.stillReady(t.PaneID) {
		return // working, blocked or gone: nothing to warn about
	}
	if s.briefVisible(t) {
		return
	}
	s.Log.Printf("%s: brief may not have been received", t.Name)
	s.notify("fleet: "+t.Name+" may not have its brief",
		"It is idle and its output does not show the brief. Check with `fleet read "+t.Name+"`; resend with `fleet send`.", "request")
}

// snippet is the first 30 characters of the brief with whitespace removed.
func snippet(brief string) string {
	r := []rune(squash(brief))
	return string(r[:min(30, len(r))])
}

// squash removes all whitespace, so text soft-wrapped by the terminal still matches.
func squash(s string) string {
	return strings.Join(strings.Fields(s), "")
}

func (s *Supervisor) setState(name, state string) {
	err := ledger.Update(name, func(t *ledger.Task) {
		if !ended(*t) { // never resurrect an exited or stopped task
			t.State = state
		}
	})
	if err != nil {
		s.Log.Printf("%s: record state: %v", name, err)
	}
}

func (s *Supervisor) notify(title, body, sound string) {
	if err := s.C.Notify(title, body, sound); err != nil {
		s.Log.Printf("notify: %v", err)
	}
}

// tail returns the last few non-empty lines of an agent's output.
func (s *Supervisor) tail(target string) string {
	txt, err := s.C.AgentRead(target, 12)
	if err != nil {
		return ""
	}
	var keep []string
	for _, l := range strings.Split(txt, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			keep = append(keep, l)
		}
	}
	if len(keep) > 5 {
		keep = keep[len(keep)-5:]
	}
	return strings.Join(keep, "\n")
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// maybeDispose stops a crewmate that is finished, meaning one of:
//   - its work is merged: its branch has commits of its own and is now contained in
//     its base (the captain merged it), or
//   - it is a one-shot task (a report, a review) and its first turn just ended
//     (turnEnded) and the agent really received its brief.
//
// Either way it must be idle and its worktree clean. crew.Stop refuses a dirty or
// unmerged worktree, so unfinished work is never discarded; the captain is told
// instead. It reports whether the crewmate was disposed.
func (s *Supervisor) maybeDispose(t ledger.Task, st herdr.AgentStatus, turnEnded bool) bool {
	if st != herdr.Idle && st != herdr.Done || s.noDispose[t.Name] {
		return false
	}
	cur, err := ledger.Load(t.Name)
	if err != nil || ended(cur) || cur.Keep || !cur.BriefSent {
		return false
	}
	reason := ""
	switch merged, _ := crew.Merged(cur); {
	case merged:
		reason = "its branch is merged into " + cur.Base
	case cur.OneShot && turnEnded && s.briefVisible(cur):
		reason = "it finished its one-shot task"
	default:
		return false
	}
	if clean, err := crew.Clean(cur.Worktree); err != nil || !clean {
		s.noDispose[t.Name] = true
		s.notify("fleet: "+t.Name+" finished, not disposed", "Reason to dispose: "+reason+", but its worktree has uncommitted changes. Look, then `fleet stop "+t.Name+"`.", "request")
		return false
	}
	if err := crew.Stop(s.C, t.Name, false); err != nil {
		s.noDispose[t.Name] = true
		s.Log.Printf("%s: not disposed: %v", t.Name, err)
		s.notify("fleet: "+t.Name+" finished, not disposed", firstLine(err.Error())+"\nReview it, then `fleet stop "+t.Name+"`.", "request")
		return false
	}
	delete(s.last, t.Name)
	s.unwatch(t.PaneID)
	s.Log.Printf("%s: disposed (%s)", t.Name, reason)
	s.notify("fleet: "+t.Name+" disposed", "Finished: "+reason+". Its report is kept: `fleet result "+t.Name+"`.", "done")
	return true
}

// briefVisible reports whether the agent's output shows its brief was received.
func (s *Supervisor) briefVisible(t ledger.Task) bool {
	txt, err := s.C.AgentRead(t.Name, 400)
	if err != nil {
		return false
	}
	return strings.Contains(squash(txt), snippet(t.Brief)) || strings.Contains(txt, "Pasted text")
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}
