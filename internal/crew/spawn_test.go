package crew

import "testing"

func TestBranchFor(t *testing.T) {
	if got := branchFor(Spec{Name: "fix-login"}); got != "fleet/fix-login" {
		t.Fatalf("default branch = %q", got)
	}
	if got := branchFor(Spec{Name: "x", Branch: "topic"}); got != "topic" {
		t.Fatalf("explicit branch = %q", got)
	}
}
