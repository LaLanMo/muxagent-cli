//go:build !darwin && !linux

package filelock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

type Lock struct {
	path string
}

func Acquire(path string, alreadyLockedMessage string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			if alreadyLockedMessage == "" {
				alreadyLockedMessage = "lock already held"
			}
			return nil, fmt.Errorf("%s", alreadyLockedMessage)
		}
		return nil, fmt.Errorf("create lock: %w", err)
	}
	_, _ = file.WriteString(strconv.Itoa(os.Getpid()))
	_ = file.Close()
	return &Lock{path: path}, nil
}

func (l *Lock) Release() error {
	if l == nil || l.path == "" {
		return nil
	}
	if err := os.Remove(l.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	l.path = ""
	return nil
}
