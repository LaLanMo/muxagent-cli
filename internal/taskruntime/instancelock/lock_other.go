//go:build !darwin && !linux

package instancelock

// Lock represents an acquired instance lock for a working directory.
type Lock struct{}

// Acquire is a no-op on unsupported platforms.
func Acquire(_ string) (*Lock, error) {
	return &Lock{}, nil
}

// Release is a no-op on unsupported platforms.
func (l *Lock) Release() error {
	return nil
}
