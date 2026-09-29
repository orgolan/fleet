package crew

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DirtyFiles lists the repo's uncommitted paths (modified, staged or untracked).
// A crewmate's worktree is a fresh checkout, so it does not contain them.
func DirtyFiles(repo string) ([]string, error) {
	out, err := exec.Command("git", "-C", repo, "status", "--porcelain").Output()
	if err != nil {
		return nil, fmt.Errorf("git status in %s: %w", repo, err)
	}
	var files []string
	for _, l := range strings.Split(string(out), "\n") {
		if len(l) > 3 {
			files = append(files, l[3:])
		}
	}
	return files, nil
}

// CopyDirty copies the repo's uncommitted changes into a worktree: tracked edits
// as a patch, untracked files as plain copies. The repo itself is never modified.
func CopyDirty(repo, worktree string) error {
	patch, err := exec.Command("git", "-C", repo, "diff", "HEAD", "--binary").Output()
	if err != nil {
		return fmt.Errorf("git diff in %s: %w", repo, err)
	}
	if len(bytes.TrimSpace(patch)) > 0 {
		apply := exec.Command("git", "-C", worktree, "apply", "--whitespace=nowarn", "-")
		apply.Stdin = bytes.NewReader(patch)
		if out, err := apply.CombinedOutput(); err != nil {
			return fmt.Errorf("apply uncommitted changes: %v: %s", err, strings.TrimSpace(string(out)))
		}
	}
	list, err := exec.Command("git", "-C", repo, "ls-files", "-z", "-o", "--exclude-standard").Output()
	if err != nil {
		return fmt.Errorf("git ls-files in %s: %w", repo, err)
	}
	for _, rel := range strings.Split(string(list), "\x00") {
		if rel == "" {
			continue
		}
		if err := copyFile(filepath.Join(repo, rel), filepath.Join(worktree, rel)); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return nil // symlinks and specials are not copied
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, st.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
