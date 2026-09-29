package crew

import "github.com/orgolan/fleet/internal/ledger"

const (
	portStart = 8900
	portBlock = 10
)

// nextPortBase picks the first block of ports no live task holds, so crewmates that
// each run a dev server or a wp-env stack never fight over 8888 and friends.
func nextPortBase() int {
	used := map[int]bool{}
	if ts, err := ledger.List(); err == nil {
		for _, t := range ts {
			if t.PortBase != 0 && t.State != "stopped" && t.State != "exited" {
				used[t.PortBase] = true
			}
		}
	}
	for b := portStart; ; b += portBlock {
		if !used[b] {
			return b
		}
	}
}
