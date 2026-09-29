package crew

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/orgolan/fleet/internal/herdr"
	"github.com/orgolan/fleet/internal/ledger"
)

// MergeOpts tunes Merge.
type MergeOpts struct {
	Test string // shell command to run in the repo with the merge applied but not committed
	Keep bool   // leave the crewmate running after the merge
}

// MergeResult says what Merge did.
type MergeResult struct {
	AlreadyMerged bool
	Stopped       bool
	Warnings      []string
}

const (
	bigFile  = 5 << 20  // a single added file over this is worth a warning
	bigTotal = 50 << 20 // and so is this much added in total
)

// Merge brings a finished crewmate's branch into its base, in the project's own
// checkout, and then stops the crewmate. It refuses anything that could lose or
// tangle work: a working or dirty crewmate, a dirty checkout, a checkout that is
// not on the base branch. The merge is made without committing first, so a
// conflict or a failing test command aborts cleanly and leaves the base untouched.
func Merge(c *herdr.Client, name string, o MergeOpts) (MergeResult, error) {
	var res MergeResult
	t, err := ledger.Load(name)
	if err != nil {
		return res, fmt.Errorf("no such task %q: %w", name, err)
	}
	if t.State == "stopped" {
		return res, fmt.Errorf("task %q is stopped", name)
	}
	if t.Base == "" || t.Branch == "" || t.Repo == "" {
		return res, fmt.Errorf("task %q has no base or branch recorded, so fleet cannot merge it", name)
	}
	if agents, err := c.Agents(); err == nil {
		for _, a := range agents {
			if a.PaneID == t.PaneID && a.Status == herdr.Working {
				return res, fmt.Errorf("%s is still working; wait for it (`fleet wait %s`)", name, name)
			}
		}
	}
	if ok, err := Clean(t.Worktree); err != nil {
		return res, fmt.Errorf("cannot inspect %s's worktree: %w", name, err)
	} else if !ok {
		return res, fmt.Errorf("%s has uncommitted changes in %s; have it commit them (`fleet send %s ...`) or discard them first", name, t.Worktree, name)
	}
	if merged, _ := Merged(t); merged {
		res.AlreadyMerged = true
	} else {
		if err := mergeInRepo(t, o, &res); err != nil {
			return res, err
		}
	}
	if !o.Keep {
		if err := Stop(c, name, false); err != nil {
			return res, fmt.Errorf("merged, but stopping %s failed: %w", name, err)
		}
		res.Stopped = true
	}
	return res, nil
}

func mergeInRepo(t ledger.Task, o MergeOpts, res *MergeResult) error {
	cur, err := gitOut(t.Repo, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", t.Repo, err)
	}
	if cur != t.Base {
		return fmt.Errorf("%s has %q checked out, not %q; check out %s there first", t.Repo, cur, t.Base, t.Base)
	}
	if ok, err := Clean(t.Repo); err != nil || !ok {
		return fmt.Errorf("%s has uncommitted changes; commit or stash them before merging", t.Repo)
	}
	res.Warnings = largeAdditions(t)
	if out, err := exec.Command("git", "-C", t.Repo, "merge", "--no-ff", "--no-commit", t.Branch).CombinedOutput(); err != nil {
		conflicts, _ := gitOut(t.Repo, "diff", "--name-only", "--diff-filter=U")
		exec.Command("git", "-C", t.Repo, "merge", "--abort").Run()
		if conflicts != "" {
			return fmt.Errorf("merge conflict in: %s\n(nothing was changed; have %s merge %s into its branch and resolve, then retry)", strings.ReplaceAll(conflicts, "\n", ", "), t.Name, t.Base)
		}
		return fmt.Errorf("git merge failed: %s", firstLine(strings.TrimSpace(string(out))))
	}
	if o.Test != "" {
		cmd := exec.Command("sh", "-c", o.Test)
		cmd.Dir = t.Repo
		if out, err := cmd.CombinedOutput(); err != nil {
			exec.Command("git", "-C", t.Repo, "merge", "--abort").Run()
			return fmt.Errorf("the test command failed, merge aborted (base untouched): %v\n%s", err, tailLines(string(out), 15))
		}
	}
	msg := fmt.Sprintf("Merge %s (%s)", t.Name, t.Branch)
	if out, err := exec.Command("git", "-C", t.Repo, "commit", "-q", "-m", msg).CombinedOutput(); err != nil {
		exec.Command("git", "-C", t.Repo, "merge", "--abort").Run()
		return fmt.Errorf("commit failed: %s", firstLine(strings.TrimSpace(string(out))))
	}
	return nil
}

// largeAdditions lists warnings about big files a merge would add to the base.
func largeAdditions(t ledger.Task) []string {
	names, err := gitOut(t.Repo, "diff", "--name-only", "--diff-filter=AM", t.Base+"..."+t.Branch)
	if err != nil || names == "" {
		return nil
	}
	var warn []string
	var total int64
	for _, f := range strings.Split(names, "\n") {
		out, err := gitOut(t.Repo, "cat-file", "-s", t.Branch+":"+f)
		if err != nil {
			continue
		}
		n, _ := strconv.ParseInt(out, 10, 64)
		total += n
		if n > bigFile {
			warn = append(warn, fmt.Sprintf("large file: %s is %.1f MB", filepath.ToSlash(f), float64(n)/(1<<20)))
		}
	}
	if total > bigTotal {
		warn = append(warn, fmt.Sprintf("the merge adds %.0f MB in total; consider whether it belongs in git", float64(total)/(1<<20)))
	}
	return warn
}

func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
