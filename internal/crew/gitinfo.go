package crew

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/orgolan/fleet/internal/ledger"
)

// GitInfo is what a worktree holds beyond its base: enough to tell a crewmate that
// finished its work from one that stopped with everything uncommitted.
type GitInfo struct {
	OK      bool // the worktree could be inspected
	Ahead   int  // commits on the branch that its base lacks; -1 when unknown
	Dirty   int  // uncommitted or untracked paths
	Last    time.Time
	HasFile bool // .fleet/report.md exists
}

// Git inspects a task's worktree. It is cheap (three git calls) and never fails:
// an unreadable worktree just reports OK false.
func Git(t ledger.Task) GitInfo {
	g := GitInfo{Ahead: -1}
	if t.Worktree == "" {
		return g
	}
	if _, err := os.Stat(t.Worktree); err != nil {
		return g
	}
	st, err := gitOut(t.Worktree, "status", "--porcelain")
	if err != nil {
		return g
	}
	g.OK = true
	if st != "" {
		g.Dirty = len(strings.Split(st, "\n"))
	}
	if t.Base != "" {
		if n, err := gitOut(t.Worktree, "rev-list", "--count", t.Base+"..HEAD"); err == nil {
			g.Ahead, _ = strconv.Atoi(n)
		}
	}
	if ts, err := gitOut(t.Worktree, "log", "-1", "--format=%ct"); err == nil {
		if sec, err := strconv.ParseInt(ts, 10, 64); err == nil {
			g.Last = time.Unix(sec, 0)
		}
	}
	_, g.HasFile = ReadReport(t.Worktree)
	return g
}

// Long is a sentence for alerts, e.g. "2 commits ahead of main, 3 uncommitted
// paths, last commit 12m ago, no report file".
func (g GitInfo) Long(base string) string {
	if !g.OK {
		return ""
	}
	var parts []string
	switch {
	case g.Ahead < 0:
	case g.Ahead == 0:
		parts = append(parts, "no commits of its own yet")
	default:
		parts = append(parts, fmt.Sprintf("%d commit(s) ahead of %s", g.Ahead, base))
	}
	if g.Dirty > 0 {
		parts = append(parts, fmt.Sprintf("%d UNCOMMITTED path(s)", g.Dirty))
	} else {
		parts = append(parts, "worktree clean")
	}
	if !g.Last.IsZero() {
		parts = append(parts, "last commit "+ago(time.Since(g.Last)))
	}
	if !g.HasFile {
		parts = append(parts, "no .fleet/report.md")
	}
	return strings.Join(parts, ", ")
}

// Short is the compact form for the status table: "+2 ~3" is two commits ahead
// and three uncommitted paths; "-" means unknown.
func (g GitInfo) Short() string {
	if !g.OK {
		return "-"
	}
	a := "?"
	if g.Ahead >= 0 {
		a = strconv.Itoa(g.Ahead)
	}
	return fmt.Sprintf("+%s ~%d", a, g.Dirty)
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
