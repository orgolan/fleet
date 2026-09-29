package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"strings"
)

// Event is one subscription event: a kind such as "pane.agent_status_changed"
// and its raw data payload.
//
// The real server names events inconsistently: subscription events (per-pane
// status) arrive as {"event":"pane.agent_status_changed","data":{flat fields}},
// while lifecycle events arrive as {"event":"pane_agent_detected","data":{
// "type":"pane_agent_detected",...}} with an underscore. Subscribe normalizes
// every Kind to the dotted form used in subscription requests.
type Event struct {
	Kind string          `json:"event"`
	Data json.RawMessage `json:"data"`
}

// NormalizeKind maps "pane_agent_detected" to "pane.agent_detected"; kinds that
// already contain a dot are returned unchanged.
func NormalizeKind(k string) string {
	if strings.Contains(k, ".") {
		return k
	}
	return strings.Replace(k, "_", ".", 1)
}

// Sub is one subscription object, e.g. {"type": "pane.exited"}. Some types need
// extra fields: pane.agent_status_changed requires "pane_id".
type Sub map[string]any

// Subscribe opens a dedicated connection, subscribes and streams events until
// ctx is cancelled or the connection drops.
// The channel is closed when the stream ends; the error (if any) is sent first on errc.
func (c *Client) Subscribe(ctx context.Context, subs ...Sub) (<-chan Event, <-chan error) {
	events, errc := make(chan Event), make(chan error, 1)
	go func() {
		defer close(events)
		defer close(errc)
		conn, err := net.Dial("unix", c.Socket)
		if err != nil {
			errc <- err
			return
		}
		defer conn.Close()
		go func() { <-ctx.Done(); conn.Close() }()

		req := request{nextID(), "events.subscribe", map[string]any{"subscriptions": subs}}
		if err := json.NewEncoder(conn).Encode(req); err != nil {
			errc <- err
			return
		}
		sc := bufio.NewScanner(conn)
		sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for sc.Scan() {
			var ev Event
			if err := json.Unmarshal(sc.Bytes(), &ev); err != nil || ev.Kind == "" {
				continue // the subscribe ack and any non-event lines
			}
			ev.Kind = NormalizeKind(ev.Kind)
			select {
			case events <- ev:
			case <-ctx.Done():
				return
			}
		}
		if ctx.Err() == nil {
			errc <- sc.Err()
		}
	}()
	return events, errc
}
