// Package fake is an in-process stand-in for the herdr socket API, enough to
// test fleet's logic without real agents or panes.
package fake

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type Agent struct {
	Name   string
	Pane   string
	Status string
	Screen string
	WS     string // workspace id, "w" unless set with SetWorkspace
}

type Server struct {
	Socket string

	mu            sync.Mutex
	agents        map[string]*Agent // by pane
	Prompts       []string          // "<name>: <text>", in order
	Notifications []string          // titles, in order
	Keys          []string          // "<target>: <key,key>" from agent.send_keys, in order
	Focused       []string          // agent.focus targets, in order
	Removed       []string          // "<workspace>" or "<workspace>!" (forced) from worktree.remove
	Runs          []string          // "<pane>: <command>" typed via pane.send_input
	Splits        []map[string]any  // pane.split params, in order
	DirtyWS       map[string]bool   // workspaces whose worktree.remove needs force
	Version       string            // ping version, "fake" if empty
	subs          []*sub
	ln            net.Listener
}

type sub struct {
	conn  net.Conn
	types map[string]string // event type -> pane filter ("" = any)
}

func New(t *testing.T) *Server {
	t.Helper()
	s := &Server{Socket: filepath.Join(t.TempDir(), "h.sock"), agents: map[string]*Agent{}}
	ln, err := net.Listen("unix", s.Socket)
	if err != nil {
		t.Fatal(err)
	}
	s.ln = ln
	t.Cleanup(func() {
		ln.Close()
		s.mu.Lock()
		for _, sb := range s.subs {
			sb.conn.Close()
		}
		s.mu.Unlock()
	})
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return s
}

// SetAgent creates or updates an agent occupying a pane.
func (s *Server) SetAgent(name, pane, status, screen string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ws := "w"
	if old := s.agents[pane]; old != nil {
		ws = old.WS
	}
	s.agents[pane] = &Agent{name, pane, status, screen, ws}
}

// SetWorkspace assigns the workspace id reported for the agent in a pane.
func (s *Server) SetWorkspace(pane, ws string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a := s.agents[pane]; a != nil {
		a.WS = ws
	}
}

// SetDirty makes worktree.remove for a workspace fail unless forced.
func (s *Server) SetDirty(ws string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.DirtyWS == nil {
		s.DirtyWS = map[string]bool{}
	}
	s.DirtyWS[ws] = true
}

func (s *Server) RemoveAgent(pane string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.agents, pane)
}

// Emit sends an event to matching subscribers.
func (s *Server) Emit(kind, pane string, data map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emitLocked(kind, pane, data)
}

func (s *Server) emitLocked(kind, pane string, data map[string]any) {
	data["pane_id"] = pane
	b, _ := json.Marshal(map[string]any{"event": kind, "data": data})
	for _, sb := range s.subs {
		if want, ok := sb.types[kind]; ok && (want == "" || want == pane) {
			sb.conn.Write(append(b, '\n'))
		}
	}
}

// Recorded returns copies of the non-prompt call logs.
func (s *Server) Recorded() (keys, focused, removed, runs []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := func(x []string) []string { return append([]string(nil), x...) }
	return c(s.Keys), c(s.Focused), c(s.Removed), c(s.Runs)
}

func (s *Server) Snapshot() (prompts, notes []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.Prompts...), append([]string(nil), s.Notifications...)
}

func (s *Server) version() string {
	if s.Version != "" {
		return s.Version
	}
	return "fake"
}

func (s *Server) byName(name string) *Agent {
	for _, a := range s.agents {
		if a.Name == name || a.Pane == name {
			return a
		}
	}
	return nil
}

func (s *Server) serve(c net.Conn) {
	sc := bufio.NewScanner(c)
	for sc.Scan() {
		var req struct {
			ID     string          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil {
			return
		}
		res, errCode := s.handle(c, req.Method, req.Params)
		var out map[string]any
		if errCode != "" {
			out = map[string]any{"id": req.ID, "error": map[string]string{"code": errCode, "message": errCode}}
		} else {
			out = map[string]any{"id": req.ID, "result": res}
		}
		b, _ := json.Marshal(out)
		s.mu.Lock()
		c.Write(append(b, '\n'))
		s.mu.Unlock()
		if req.Method == "events.subscribe" {
			continue // keep the connection open for events
		}
		return
	}
}

func (s *Server) handle(c net.Conn, method string, raw json.RawMessage) (any, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch method {
	case "ping":
		return map[string]any{"type": "pong", "version": s.version(), "protocol": 22}, ""
	case "agent.list":
		var list []map[string]any
		for _, a := range s.agents {
			list = append(list, map[string]any{"agent": "claude", "agent_status": a.Status, "pane_id": a.Pane, "workspace_id": a.WS})
		}
		return map[string]any{"type": "agent_list", "agents": list}, ""
	case "agent.read":
		var p struct{ Target string }
		json.Unmarshal(raw, &p)
		a := s.byName(p.Target)
		if a == nil {
			return nil, "agent_not_found"
		}
		return map[string]any{"type": "pane_read", "read": map[string]any{"text": a.Screen}}, ""
	case "agent.prompt":
		var p struct{ Target, Text string }
		json.Unmarshal(raw, &p)
		a := s.byName(p.Target)
		switch {
		case a == nil:
			return nil, "agent_not_ready"
		case a.Status == "blocked":
			return nil, "agent_blocked"
		}
		s.Prompts = append(s.Prompts, a.Name+": "+p.Text)
		a.Status = "working"
		return map[string]any{"type": "agent_prompted"}, ""
	case "agent.send_keys":
		var p struct {
			Target string
			Keys   []string
		}
		json.Unmarshal(raw, &p)
		a := s.byName(p.Target)
		if a == nil {
			return nil, "agent_not_found"
		}
		s.Keys = append(s.Keys, a.Name+": "+strings.Join(p.Keys, ","))
		return map[string]any{"type": "ok"}, ""
	case "agent.focus":
		var p struct{ Target string }
		json.Unmarshal(raw, &p)
		a := s.byName(p.Target)
		if a == nil {
			return nil, "agent_not_found"
		}
		s.Focused = append(s.Focused, a.Name)
		return map[string]any{"type": "ok"}, ""
	case "worktree.remove":
		var p struct {
			WorkspaceID string `json:"workspace_id"`
			Force       bool
		}
		json.Unmarshal(raw, &p)
		if s.DirtyWS[p.WorkspaceID] && !p.Force {
			return nil, "worktree_dirty"
		}
		rec := p.WorkspaceID
		if p.Force {
			rec += "!"
		}
		s.Removed = append(s.Removed, rec)
		// Like herdr, removing the worktree closes its workspace's panes.
		for pane, a := range s.agents {
			if a.WS == p.WorkspaceID {
				delete(s.agents, pane)
				s.emitLocked("pane.closed", pane, map[string]any{"workspace_id": p.WorkspaceID})
			}
		}
		return map[string]any{"type": "worktree_removed", "workspace_id": p.WorkspaceID, "path": "", "forced": p.Force}, ""
	case "pane.split":
		var p map[string]any
		json.Unmarshal(raw, &p)
		s.Splits = append(s.Splits, p)
		return map[string]any{"type": "pane_info", "pane": map[string]any{"pane_id": "w:new", "workspace_id": "w", "tab_id": "w:t1"}}, ""
	case "pane.send_input":
		var p struct {
			PaneID string `json:"pane_id"`
			Text   string
			Keys   []string
		}
		json.Unmarshal(raw, &p)
		s.Runs = append(s.Runs, p.PaneID+": "+p.Text+" "+strings.Join(p.Keys, ","))
		return map[string]any{"type": "ok"}, ""
	case "notification.show":
		var p struct{ Title string }
		json.Unmarshal(raw, &p)
		s.Notifications = append(s.Notifications, p.Title)
		return map[string]any{"type": "notification_show", "shown": true}, ""
	case "events.subscribe":
		var p struct {
			Subscriptions []struct {
				Type   string `json:"type"`
				PaneID string `json:"pane_id"`
			}
		}
		json.Unmarshal(raw, &p)
		sb := &sub{conn: c, types: map[string]string{}}
		for _, x := range p.Subscriptions {
			sb.types[x.Type] = x.PaneID
		}
		s.subs = append(s.subs, sb)
		return map[string]any{"type": "subscription_started"}, ""
	}
	return nil, "unknown_method"
}
