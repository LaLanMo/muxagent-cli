//go:build darwin || linux

package filelock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"
)

type Lock struct {
	file *os.File
	path string
}

func Acquire(path string, alreadyLockedMessage string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock: %w", err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		if err == unix.EWOULDBLOCK {
			if alreadyLockedMessage == "" {
				alreadyLockedMessage = "lock already held"
			}
			return nil, fmt.Errorf("%s", alreadyLockedMessage)
		}
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	_ = file.Truncate(0)
	_, _ = file.WriteString(strconv.Itoa(os.Getpid()))
	return &Lock{file: file, path: path}, nil
}

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
