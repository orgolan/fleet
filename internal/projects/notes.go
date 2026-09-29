package projects

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Notes are the first mate's memory of a project, kept in projects/<name>/notes.md
// in two sections of one-line bullets:
//
//	## Conventions   stable facts (build/test commands, rules). Always sent to crewmates.
//	## Log           dated lessons and outcomes. Only the newest are sent.
//
// Entries are numbered across both sections (conventions first) for --edit and --rm.

// Section names an entry's section.
type Section string

const (
	Conventions Section = "Conventions"
	Log         Section = "Log"
)

const (
	// BriefLogEntries is how many of the newest log entries go into a brief.
	BriefLogEntries = 10
	// WarnChars is the notes size past which the captain is told to trim them.
	WarnChars = 3000
)

// Entry is one numbered note.
type Entry struct {
	Num     int
	Section Section
	Text    string // without the leading "- "
}

// NoteFile is a parsed notes.md. Lines other than bullets (comments, blank lines)
// are kept so a rewrite loses nothing.
type NoteFile struct {
	name string
	pre  []string // everything before the first known section
	conv []string // lines of the Conventions section
	log  []string // lines of the Log section
}

func newNotes(name string) string {
	return fmt.Sprintf("# %s\n\nNotes for the first mate. Conventions are always sent to crewmates; only the newest\nlog entries are.\n\n## Conventions\n\n## Log\n", name)
}

func isBullet(l string) bool { return strings.HasPrefix(l, "- ") }

// LoadNotes reads and parses a project's notes. A missing file is empty notes.
// Bullets found before any section (the old flat format) are treated as log entries.
func LoadNotes(name string) (*NoteFile, error) {
	if _, err := Get(name); err != nil {
		return nil, err
	}
	dir, _ := entryDir(name)
	b, err := os.ReadFile(filepath.Join(dir, "notes.md"))
	if errors.Is(err, os.ErrNotExist) {
		b = []byte(newNotes(name))
	} else if err != nil {
		return nil, err
	}
	nf := &NoteFile{name: name}
	cur := &nf.pre
	for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		switch strings.TrimSpace(l) {
		case "## Conventions":
			cur = &nf.conv
			continue
		case "## Log":
			cur = &nf.log
			continue
		}
		*cur = append(*cur, l)
	}
	var keep []string
	for _, l := range nf.pre {
		if isBullet(l) {
			nf.log = append(nf.log, l)
		} else {
			keep = append(keep, l)
		}
	}
	nf.pre = keep
	return nf, nil
}

// Entries lists every note, numbered from 1.
func (nf *NoteFile) Entries() []Entry {
	var out []Entry
	for _, sec := range []struct {
		s     Section
		lines []string
	}{{Conventions, nf.conv}, {Log, nf.log}} {
		for _, l := range sec.lines {
			if isBullet(l) {
				out = append(out, Entry{Num: len(out) + 1, Section: sec.s, Text: strings.TrimPrefix(l, "- ")})
			}
		}
	}
	return out
}

var datePrefix = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}: `)

// Add appends a note. Log entries get today's date.
func (nf *NoteFile) Add(sec Section, text string) error {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return fmt.Errorf("empty note")
	}
	if sec == Log {
		nf.log = append(nf.log, "- "+time.Now().Format("2006-01-02")+": "+text)
	} else {
		nf.conv = append(nf.conv, "- "+text)
	}
	return nil
}

// locate finds the line holding entry n and the slice it lives in.
func (nf *NoteFile) locate(n int) (lines *[]string, idx int, err error) {
	count := 0
	for _, lp := range []*[]string{&nf.conv, &nf.log} {
		for i, l := range *lp {
			if isBullet(l) {
				count++
				if count == n {
					return lp, i, nil
				}
			}
		}
	}
	return nil, 0, fmt.Errorf("no note %d (there are %d)", n, count)
}

// Remove deletes entry n.
func (nf *NoteFile) Remove(n int) error {
	lp, i, err := nf.locate(n)
	if err != nil {
		return err
	}
	*lp = append((*lp)[:i], (*lp)[i+1:]...)
	return nil
}

// Edit replaces the text of entry n, keeping a log entry's original date.
func (nf *NoteFile) Edit(n int, text string) error {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return fmt.Errorf("empty note")
	}
	lp, i, err := nf.locate(n)
	if err != nil {
		return err
	}
	old := strings.TrimPrefix((*lp)[i], "- ")
	if d := datePrefix.FindString(old); d != "" && !datePrefix.MatchString(text) {
		text = d + text
	}
	(*lp)[i] = "- " + text
	return nil
}

// String renders the notes file.
func (nf *NoteFile) String() string {
	var b strings.Builder
	trim := func(ls []string) []string {
		for len(ls) > 0 && strings.TrimSpace(ls[len(ls)-1]) == "" {
			ls = ls[:len(ls)-1]
		}
		return ls
	}
	for _, l := range trim(nf.pre) {
		b.WriteString(l + "\n")
	}
	b.WriteString("\n## Conventions\n")
	for _, l := range trim(nf.conv) {
		b.WriteString(l + "\n")
	}
	b.WriteString("\n## Log\n")
	for _, l := range trim(nf.log) {
		b.WriteString(l + "\n")
	}
	return b.String()
}

// Save writes the notes back.
func (nf *NoteFile) Save() error {
	dir, err := entryDir(nf.name)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "notes.md"), []byte(nf.String()), 0o644)
}

// Size is the rendered length in characters.
func (nf *NoteFile) Size() int { return len([]rune(nf.String())) }

// Brief renders the notes for a crewmate's brief: every convention and the newest
// BriefLogEntries log entries. It returns "" when there is nothing to send.
func (nf *NoteFile) Brief() string {
	var conv, logs []string
	for _, e := range nf.Entries() {
		if e.Section == Conventions {
			conv = append(conv, "- "+e.Text)
		} else {
			logs = append(logs, "- "+e.Text)
		}
	}
	if len(conv) == 0 && len(logs) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Project notes (%s):\n", nf.name)
	if len(conv) > 0 {
		b.WriteString("Conventions:\n" + strings.Join(conv, "\n") + "\n")
	}
	if len(logs) > 0 {
		omitted := 0
		if len(logs) > BriefLogEntries {
			omitted = len(logs) - BriefLogEntries
			logs = logs[omitted:]
		}
		b.WriteString("Recent log:\n" + strings.Join(logs, "\n") + "\n")
		if omitted > 0 {
			fmt.Fprintf(&b, "(%d older log entries omitted)\n", omitted)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
