package supervisor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ErrRunning means another supervisor holds the single-instance lock.
var ErrRunning = errors.New("a supervisor is already running")

// Lock is the single-instance lock: an flock on <dir>/supervisor.lock. The
// kernel drops it when the process dies, so a crash never leaves it stale.
type Lock struct{ f *os.File }

func lockPaths(dir string) (lock, pid string) {
	return filepath.Join(dir, "supervisor.lock"), filepath.Join(dir, "supervisor.pid")
}

func tryFlock(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	lp, _ := lockPaths(dir)
	f, err := os.OpenFile(lp, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrRunning
		}
		return nil, err
	}
	return f, nil
}

// Acquire takes the lock and records this process's pid. When another
// supervisor holds it, the error wraps ErrRunning and names its pid if known.
func Acquire(dir string) (*Lock, error) {
	f, err := tryFlock(dir)
	if errors.Is(err, ErrRunning) {
		if pid := ReadPID(dir); pid > 0 {
			return nil, fmt.Errorf("%w (pid %d)", ErrRunning, pid)
		}
		return nil, ErrRunning
	}
	if err != nil {
		return nil, err
	}
	_, pp := lockPaths(dir)
	if err := os.WriteFile(pp, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		f.Close()
		return nil, err
	}
	return &Lock{f}, nil
}

// Release drops the lock early; exiting the process does the same.
func (l *Lock) Release() error { return l.f.Close() }

// Running reports whether a supervisor holds the lock. It probes with a
// non-blocking flock and releases it again at once.
func Running(dir string) (bool, error) {
	f, err := tryFlock(dir)
	if errors.Is(err, ErrRunning) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	f.Close()
	return false, nil
}

// ReadPID returns the pid recorded by the lock holder, or 0.
func ReadPID(dir string) int {
	_, pp := lockPaths(dir)
	b, err := os.ReadFile(pp)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}
