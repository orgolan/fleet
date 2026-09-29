package crew

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/orgolan/fleet/internal/ledger"
)

// Long briefs travel by file, not by paste: a brief typed into Claude Code's input
// becomes a "[Pasted text]" placeholder that can sit unsubmitted, and it cannot be
// checked against the screen. The crewmate is told to read .fleet/brief.md instead
// and to leave its final report in .fleet/report.md, which outlives its terminal.
const (
	fleetDir   = ".fleet"
	briefFile  = "brief.md"
	reportFile = "report.md"
	inlineMax  = 300 // longest brief sent as typed text
)

const filePrompt = "Your task brief is in .fleet/brief.md: read it in full and carry it out. " +
	"When you are done, commit your work, write your final report (what you did, what you verified, " +
	"what you could not do or verify, decisions the captain should review) to .fleet/report.md, " +
	"and reply with a one-line summary."

// PromptText is what is typed into a crewmate's input for the task's brief: the
// brief itself when it is short, else a pointer to the brief file.
func PromptText(t ledger.Task) string {
	if t.Worktree == "" || (len(t.Brief) <= inlineMax && !strings.Contains(t.Brief, "\n")) {
		return t.Brief
	}
	return filePrompt
}

// writeBriefFile puts the brief in the worktree, out of git's sight.
func writeBriefFile(t ledger.Task) error {
	dir := filepath.Join(t.Worktree, fleetDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, briefFile), []byte(briefFileContent(t)), 0o644); err != nil {
		return err
	}
	return excludeFleetDir(t.Worktree)
}

func briefFileContent(t ledger.Task) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Task brief: %s\n\n", t.Name)
	b.WriteString(t.Brief)
	if t.PortBase > 0 {
		fmt.Fprintf(&b, "\n\n---\n## Ports\nThis task owns ports %d-%d (FLEET_PORT_BASE=%d). Use only these for anything you serve, "+
			"because other crewmates run their own servers at the same time: for wp-env write a gitignored "+
			".wp-env.override.json with {\"port\": %d, \"testsPort\": %d}; for a dev server use %d.\n",
			t.PortBase, t.PortBase+portBlock-1, t.PortBase, t.PortBase, t.PortBase+1, t.PortBase)
	}
	b.WriteString("\n\n---\nWhen you finish, write your final report to .fleet/report.md in this worktree " +
		"(the .fleet/ directory is not tracked by git).\n")
	return b.String()
}

// excludeFleetDir keeps .fleet/ out of `git status` without touching the repo's files.
func excludeFleetDir(worktree string) error {
	p, err := gitOut(worktree, "rev-parse", "--git-path", "info/exclude")
	if err != nil || p == "" {
		return err
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(worktree, p)
	}
	cur, _ := os.ReadFile(p)
	for _, l := range strings.Split(string(cur), "\n") {
		if strings.TrimSpace(l) == fleetDir+"/" {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	sep := ""
	if len(cur) > 0 && !strings.HasSuffix(string(cur), "\n") {
		sep = "\n"
	}
	_, err = f.WriteString(sep + fleetDir + "/\n")
	return err
}

// ReadReport returns the report a crewmate left in its worktree, if any.
func ReadReport(worktree string) (string, bool) {
	if worktree == "" {
		return "", false
	}
	b, err := os.ReadFile(filepath.Join(worktree, fleetDir, reportFile))
	if err != nil || strings.TrimSpace(string(b)) == "" {
		return "", false
	}
	return string(b), true
}
