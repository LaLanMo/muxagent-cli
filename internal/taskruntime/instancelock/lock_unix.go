//go:build darwin || linux

package instancelock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"
)

const lockFileName = "instance.lock"

// Lock represents an acquired instance lock for a working directory.
type Lock struct {
	file *os.File
	path string
}

// Acquire takes an exclusive, non-blocking flock on <workDir>/.muxagent/instance.lock.
// If another process already holds the lock, it returns an error.
// The lock is automatically released by the kernel if the process exits.
func Acquire(workDir string) (*Lock, error) {
	dir := filepath.Join(workDir, ".muxagent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}
	path := filepath.Join(dir, lockFileName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open instance lock: %w", err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		if err == unix.EWOULDBLOCK {
			return nil, fmt.Errorf("another muxagent instance is already running in this directory")
		}
		return nil, fmt.Errorf("acquire instance lock: %w", err)
	}
	// Write PID for informational purposes.
	_ = file.Truncate(0)
	_, _ = file.WriteString(strconv.Itoa(os.Getpid()))

	return &Lock{file: file, path: path}, nil
}

// Release releases the lock and removes the lock file.
// It is safe to call on a nil receiver.
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	removeErr := os.Remove(l.path)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return removeErr
}
