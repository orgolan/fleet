package crew

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestCopyDirtyBringsEditsAndUntrackedFilesWithoutTouchingRepo(t *testing.T) {
	repo, wt := t.TempDir(), t.TempDir()
	git(t, repo, "init", "-q")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\n"), 0o644)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "c")
	git(t, wt, "init", "-q")
	os.WriteFile(filepath.Join(wt, "a.txt"), []byte("one\n"), 0o644) // a checkout of the same commit
	git(t, wt, "add", ".")
	git(t, wt, "commit", "-q", "-m", "c")

	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\ntwo\n"), 0o644)
	os.MkdirAll(filepath.Join(repo, "css"), 0o755)
	os.WriteFile(filepath.Join(repo, "css", "new.css"), []byte("body{}\n"), 0o644)

	if files, err := DirtyFiles(repo); err != nil || len(files) != 2 {
		t.Fatalf("DirtyFiles = %v, %v", files, err)
	}
	if err := CopyDirty(repo, wt); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(wt, "a.txt")); string(b) != "one\ntwo\n" {
		t.Fatalf("a.txt in worktree = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(wt, "css", "new.css")); string(b) != "body{}\n" {
		t.Fatalf("new.css in worktree = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "a.txt")); string(b) != "one\ntwo\n" {
		t.Fatalf("repo file changed: %q", b)
	}
}
