//go:build darwin || linux

package update

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

type updateLock struct {
	file *os.File
}

func acquireUpdateLock(path string) (*updateLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open update lock: %w", err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		if err == unix.EWOULDBLOCK {
			return nil, fmt.Errorf("another update is already running")
		}
		return nil, fmt.Errorf("acquire update lock: %w", err)
	}
	return &updateLock{file: file}, nil
}

func (l *updateLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	closeErr := l.file.Close()
	if err != nil {
		return err
	}
	return closeErr
}
