package main

import (
	"os"
	"testing"
)

// Every test sets its own FLEET_HOME; this one is the net under them. A goroutine
// that outlives its test (and its env override) must never write to the captain's
// real ledger, so the whole process starts in a throwaway state directory.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fleet-test-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("FLEET_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
