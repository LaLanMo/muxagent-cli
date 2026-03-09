//go:build !darwin && !linux

package update

import "fmt"

type updateLock struct{}

func acquireUpdateLock(path string) (*updateLock, error) {
	return nil, fmt.Errorf("self-update is not supported on this platform")
}

func (l *updateLock) Close() error {
	return nil
}
