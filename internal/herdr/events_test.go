package herdr

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
)

// The fixture holds lines captured from a real herdr server.
func TestRealEventShapes(t *testing.T) {
	f, err := os.Open("testdata/events.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	kinds := map[string]int{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var ev Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil || ev.Kind == "" {
			kinds["(ack)"]++
			continue
		}
		ev.Kind = NormalizeKind(ev.Kind)
		kinds[ev.Kind]++
		switch ev.Kind {
		case "pane.agent_status_changed":
			var d AgentStatusChanged
			if err := json.Unmarshal(ev.Data, &d); err != nil || d.PaneID != "wX:p2" || d.Status == "" {
				t.Errorf("bad status data %s: %+v %v", ev.Data, d, err)
			}
		case "pane.agent_detected", "pane.exited":
			var d struct {
				PaneID string `json:"pane_id"`
			}
			if json.Unmarshal(ev.Data, &d) != nil || d.PaneID == "" {
				t.Errorf("%s: no pane_id in %s", ev.Kind, ev.Data)
			}
		}
	}
	for _, k := range []string{"pane.agent_detected", "pane.agent_status_changed", "pane.exited", "pane.created", "(ack)"} {
		if kinds[k] == 0 {
			t.Errorf("fixture has no %s line: %v", k, kinds)
		}
	}
}

func TestNormalizeKind(t *testing.T) {
	for in, want := range map[string]string{
		"pane_agent_detected":       "pane.agent_detected",
		"pane.agent_status_changed": "pane.agent_status_changed",
		"pane_exited":               "pane.exited",
	} {
		if got := NormalizeKind(in); got != want {
			t.Errorf("NormalizeKind(%q) = %q, want %q", in, got, want)
		}
	}
}
