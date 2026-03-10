// Package localkey provides OS keychain-backed local encryption key derivation.
// It stores a random 32-byte master key in the OS secure storage (macOS Keychain /
// Linux Secret Service) and derives domain-specific keys via HKDF.
package localkey

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/LaLanMo/muxagent-cli/internal/crypto"
)

const (
	keychainService = "muxagent"
	keychainAccount = "master-key"
	masterKeySize   = 32
)

type keychainBackend interface {
	Get() (string, error)
	Set(string) error
	Delete() error
	IsNotFound(error) bool
}

var (
	masterKeyOnce  sync.Once
	cachedKey      *[32]byte
	cachedErr      error
	currentBackend keychainBackend = osKeychainBackend{}
)

// DeriveKey derives a 32-byte key for the given info string from the OS keychain-backed
// master key. Different info strings produce different keys (HKDF domain separation).
func DeriveKey(info string) (*[32]byte, error) {
	master, err := getMasterKey()
	if err != nil {
		return nil, err
	}
	return crypto.DeriveKeyFromBytes(master[:], []byte(info))
}

// DeleteMasterKey removes the master key from the OS keychain.
// Only for explicit "wipe all state" paths.
func DeleteMasterKey() error {
	if err := currentBackend.Delete(); err != nil {
		return err
	}
	ResetCachedKey()
	return nil
}

// ResetCachedKey clears the cached master key, forcing the next DeriveKey call
// to re-read from the keychain. Primarily for testing.
func ResetCachedKey() {
	masterKeyOnce = sync.Once{}
	cachedKey = nil
	cachedErr = nil
}

func getMasterKey() (*[32]byte, error) {
	masterKeyOnce.Do(func() {
		cachedKey, cachedErr = loadOrCreateMasterKey()
	})
	return cachedKey, cachedErr
}

func loadOrCreateMasterKey() (*[32]byte, error) {
	backend := currentBackend

	// Try to load existing key
	stored, err := backend.Get()
	if err == nil {
		return decodeMasterKey(stored)
	}

	if !backend.IsNotFound(err) {
		return nil, fmt.Errorf("OS keychain unavailable: %w\n"+
			"On macOS ensure Keychain Access is enabled. "+
			"On Linux ensure a Secret Service provider (gnome-keyring, KeePassXC) is running.", err)
	}

	// Not found — generate new key
	var key [masterKeySize]byte
	if _, err := io.ReadFull(rand.Reader, key[:]); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}

	encoded := hex.EncodeToString(key[:])
	if err := backend.Set(encoded); err != nil {
		return nil, fmt.Errorf("OS keychain unavailable: %w\n"+
			"On macOS ensure Keychain Access is enabled. "+
			"On Linux ensure a Secret Service provider (gnome-keyring, KeePassXC) is running.", err)
	}

	return &key, nil
}

func setBackendForTesting(backend keychainBackend) func() {
	previous := currentBackend
	currentBackend = backend
	ResetCachedKey()
	return func() {
		currentBackend = previous
		ResetCachedKey()
	}
}

func decodeMasterKey(encoded string) (*[32]byte, error) {
	b, err := hex.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("corrupt master key in keychain: %w", err)
	}
	if len(b) != masterKeySize {
		return nil, errors.New("corrupt master key in keychain: wrong size")
	}
	var key [masterKeySize]byte
	copy(key[:], b)
	return &key, nil
}
