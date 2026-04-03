//go:build darwin || linux

package instancelock

import (
	"path/filepath"

	"github.com/LaLanMo/muxagent-cli/internal/filelock"
)

const lockFileName = "instance.lock"

// Lock represents an acquired instance lock for a working directory.
type Lock struct {
	lock *filelock.Lock
}

// Acquire takes an exclusive, non-blocking flock on <workDir>/.muxagent/instance.lock.
// If another process already holds the lock, it returns an error.
// The lock is automatically released by the kernel if the process exits.
func Acquire(workDir string) (*Lock, error) {
	dir := filepath.Join(workDir, ".muxagent")
	path := filepath.Join(dir, lockFileName)
	lock, err := filelock.Acquire(path, "another muxagent instance is already running in this directory")
	if err != nil {
		return nil, err
	}
	return &Lock{lock: lock}, nil
}

// Release releases the lock and removes the lock file.
// It is safe to call on a nil receiver.
func (l *Lock) Release() error {
	if l == nil || l.lock == nil {
		return nil
	}
	err := l.lock.Release()
	l.lock = nil
	return err
}
