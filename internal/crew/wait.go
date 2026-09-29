package crew

import (
	"errors"
	"fmt"
	"time"

	"github.com/orgolan/fleet/internal/herdr"
)

// ErrTimeout means Wait gave up before the crewmates settled.
var ErrTimeout = errors.New("timed out waiting for the crew")

// settled reports whether a crewmate is at rest: not working and not waiting for
// its brief to be delivered. Blocked counts, because it needs the captain, not time.
func settled(r Row) bool {
	return r.Live != "working" && r.Brief != "pending"
}

// Wait blocks until the named crewmates (every live one when none are named)
// have settled: all of them when all is set, else the first. It returns the rows
// it was watching as of its last look, and ErrTimeout when timeout passes first.
func Wait(c *herdr.Client, names []string, all bool, timeout, poll time.Duration) ([]Row, error) {
	deadline := time.Now().Add(timeout)
	for {
		rows, err := Status(c)
		if err != nil {
			return nil, err
		}
		watched, err := pick(rows, names)
		if err != nil {
			return nil, err
		}
		if len(watched) == 0 {
			return watched, nil
		}
		n := 0
		for _, r := range watched {
			if settled(r) {
				n++
			}
		}
		if (all && n == len(watched)) || (!all && n > 0) {
			return watched, nil
		}
		if timeout > 0 && time.Now().After(deadline) {
			return watched, ErrTimeout
		}
		time.Sleep(poll)
	}
}

// pick selects the rows to watch: the named tasks, or every task with a live agent.
func pick(rows []Row, names []string) ([]Row, error) {
	if len(names) == 0 {
		var live []Row
		for _, r := range rows {
			if r.Live != "-" {
				live = append(live, r)
			}
		}
		return live, nil
	}
	byName := map[string]Row{}
	for _, r := range rows {
		byName[r.Name] = r
	}
	var out []Row
	for _, n := range names {
		r, ok := byName[n]
		if !ok {
			return nil, fmt.Errorf("no such task %q", n)
		}
		out = append(out, r)
	}
	return out, nil
}
