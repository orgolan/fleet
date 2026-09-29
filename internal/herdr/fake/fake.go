// Package fake is an in-process stand-in for the herdr socket API, enough to
// test fleet's logic without real agents or panes.
package fake

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"sync"
	"testing"
)

type Agent struct {
	Name   string
	Pane   string
	Status string
	Screen string
}

type Server struct {
	Socket string

	mu            sync.Mutex
	agents        map[string]*Agent // by pane
	Prompts       []string          // "<name>: <text>", in order
	Notifications []string          // titles, in order
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
	s.agents[pane] = &Agent{name, pane, status, screen}
}

func (s *Server) RemoveAgent(pane string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.agents, pane)
}

// Emit sends an event to matching subscribers.
func (s *Server) Emit(kind, pane string, data map[string]any) {
	data["pane_id"] = pane
	b, _ := json.Marshal(map[string]any{"event": kind, "data": data})
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sb := range s.subs {
		if want, ok := sb.types[kind]; ok && (want == "" || want == pane) {
			sb.conn.Write(append(b, '\n'))
		}
	}
}

func (s *Server) Snapshot() (prompts, notes []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.Prompts...), append([]string(nil), s.Notifications...)
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
		return map[string]any{"type": "pong", "version": "fake", "protocol": 22}, ""
	case "agent.list":
		var list []map[string]any
		for _, a := range s.agents {
			list = append(list, map[string]any{"agent": "claude", "agent_status": a.Status, "pane_id": a.Pane, "workspace_id": "w"})
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
