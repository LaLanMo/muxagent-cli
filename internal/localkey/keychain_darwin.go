//go:build darwin

package localkey

import (
	"github.com/zalando/go-keyring"
)

type osKeychainBackend struct{}

func (osKeychainBackend) Get() (string, error) {
	return keyring.Get(keychainService, keychainAccount)
}

func (osKeychainBackend) Set(value string) error {
	return keyring.Set(keychainService, keychainAccount, value)
}

func (osKeychainBackend) Delete() error {
	return keyring.Delete(keychainService, keychainAccount)
}

func (osKeychainBackend) IsNotFound(err error) bool {
	return err == keyring.ErrNotFound
}
