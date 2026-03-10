//go:build !darwin && !linux

package localkey

import "errors"

var errUnsupportedPlatform = errors.New("OS keychain not supported on this platform")

type osKeychainBackend struct{}

func (osKeychainBackend) Get() (string, error) {
	return "", errUnsupportedPlatform
}

func (osKeychainBackend) Set(value string) error {
	return errUnsupportedPlatform
}

func (osKeychainBackend) Delete() error {
	return errUnsupportedPlatform
}

func (osKeychainBackend) IsNotFound(err error) bool {
	return false
}
