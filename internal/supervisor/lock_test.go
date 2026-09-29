package supervisor

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestSingleInstanceLock(t *testing.T) {
	dir := t.TempDir()
	if r, err := Running(dir); err != nil || r {
		t.Fatalf("Running before = %v, %v", r, err)
	}
	l, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := Running(dir); !r {
		t.Fatal("Running should be true while held")
	}
	_, err = Acquire(dir)
	if !errors.Is(err, ErrRunning) || !strings.Contains(err.Error(), strconv.Itoa(os.Getpid())) {
		t.Fatalf("second Acquire err = %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	if r, _ := Running(dir); r {
		t.Fatal("Running should be false after release; probe must not leave it held")
	}
	if l2, err := Acquire(dir); err != nil {
		t.Fatal(err)
	} else {
		l2.Release()
	}
}
