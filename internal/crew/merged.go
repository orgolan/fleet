package crew

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/orgolan/fleet/internal/ledger"
)

func gitOut(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

// resolveBase returns the ref a new branch will be cut from and its commit. An
// empty base means the repo's current branch; a detached HEAD gives ("", "") and
// disables merge detection.
func resolveBase(repo, base string) (ref, rev string) {
	if base == "" {
		b, err := gitOut(repo, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil || b == "" || b == "HEAD" {
			return "", ""
		}
		base = b
	}
	rev, err := gitOut(repo, "rev-parse", "--verify", "-q", base+"^{commit}")
	if err != nil {
		return "", ""
	}
	return base, rev
}

// Merged reports whether a crewmate's work has been merged: its branch has commits
// of its own (it moved past the commit it was cut from) and is now contained in
// its base. A branch that never moved is not "merged", it is just new.
func Merged(t ledger.Task) (bool, error) {
	if t.Base == "" || t.BaseRev == "" || t.Branch == "" {
		return false, nil
	}
	tip, err := gitOut(t.Repo, "rev-parse", "--verify", "-q", t.Branch+"^{commit}")
	if err != nil {
		return false, nil // branch gone
	}
	if tip == t.BaseRev {
		return false, nil
	}
	if _, err := exec.Command("git", "-C", t.Repo, "merge-base", "--is-ancestor", tip, t.Base).Output(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("merge-base in %s: %w", t.Repo, err)
	}
	return true, nil
}

// Clean reports whether a worktree has no uncommitted or untracked changes.
func Clean(worktree string) (bool, error) {
	out, err := gitOut(worktree, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out == "", nil
}
