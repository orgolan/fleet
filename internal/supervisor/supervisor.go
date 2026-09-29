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

	last    map[string]herdr.AgentStatus // task name -> last handled status
	seen    map[string]bool              // task name -> was ever observed live
	watches map[string]context.CancelFunc
	status  chan herdr.AgentStatusChanged
}

func New(c *herdr.Client, l *log.Logger) *Supervisor {
	return &Supervisor{
		C: c, Log: l, Poll: 20 * time.Second,
		last: map[string]herdr.AgentStatus{}, seen: map[string]bool{},
		watches: map[string]context.CancelFunc{},
		status:  make(chan herdr.AgentStatusChanged, 64),
	}
}

// Run blocks until ctx is cancelled.
func (s *Supervisor) Run(ctx context.Context) error {
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
			s.deliver(t)
			return
		}
		s.setState(t.Name, string(st))
		if prev == herdr.Working {
			s.notify("fleet: "+t.Name+" finished a turn", s.tail(t.Name), "done")
		}
	default:
		s.setState(t.Name, string(st))
	}
}

func (s *Supervisor) deliver(t ledger.Task) {
	err := crew.Deliver(s.C, t.Name, false, false)
	switch {
	case err == nil:
		s.Log.Printf("%s: brief delivered", t.Name)
		s.last[t.Name] = herdr.Working
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
