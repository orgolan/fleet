package claudetrust

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setup(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	cfg := filepath.Join(dir, ".claude.json")
	if content != "" {
		if err := os.WriteFile(cfg, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return cfg
}

func TestTrustSetsOnlyItsKeyAndKeepsTheRest(t *testing.T) {
	cfg := setup(t, `{"userID":"a&b<c>","bigNumber":1759000000000123,"projects":{"/x":{"hasTrustDialogAccepted":false,"allowedTools":["Bash"]},"/other":{"hasTrustDialogAccepted":true}}}`)
	repo := t.TempDir()
	if ok, _ := IsTrusted(repo); ok {
		t.Fatal("untrusted path reported trusted")
	}
	if err := Trust(repo); err != nil {
		t.Fatal(err)
	}
	if ok, err := IsTrusted(repo); err != nil || !ok {
		t.Fatalf("IsTrusted after Trust = %v, %v", ok, err)
	}
	b, _ := os.ReadFile(cfg)
	s := string(b)
	for _, want := range []string{`"a&b<c>"`, `1759000000000123`, `"allowedTools"`, `"/other"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("rewrite lost %s:\n%s", want, s)
		}
	}
	if st, _ := os.Stat(cfg); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", st.Mode().Perm())
	}
	if ok, _ := IsTrusted("/x"); ok {
		t.Fatal("unrelated /x became trusted")
	}
}

func TestTrustCreatesMissingConfigAndIsIdempotent(t *testing.T) {
	cfg := setup(t, "")
	repo := t.TempDir()
	for i := 0; i < 2; i++ {
		if err := Trust(repo); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(cfg); err != nil {
		t.Fatal(err)
	}
	if ok, _ := IsTrusted(repo); !ok {
		t.Fatal("not trusted")
	}
}

func TestTrustRefusesACorruptConfigWithoutTouchingIt(t *testing.T) {
	cfg := setup(t, `{not json`)
	if err := Trust(t.TempDir()); err == nil {
		t.Fatal("corrupt config accepted")
	}
	if b, _ := os.ReadFile(cfg); string(b) != `{not json` {
		t.Fatalf("corrupt config was modified: %q", b)
	}
}
