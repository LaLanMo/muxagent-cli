package localkey

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errFakeNotFound = errors.New("fake not found")

type fakeBackend struct {
	stored    string
	getErr    error
	setErr    error
	deleteErr error
	getCalls  int
	setCalls  int
}

func (f *fakeBackend) Get() (string, error) {
	f.getCalls++
	if f.getErr != nil {
		return "", f.getErr
	}
	return f.stored, nil
}

func (f *fakeBackend) Set(value string) error {
	f.setCalls++
	if f.setErr != nil {
		return f.setErr
	}
	f.getErr = nil
	f.stored = value
	return nil
}

func (f *fakeBackend) Delete() error {
	f.stored = ""
	f.getErr = errFakeNotFound
	return f.deleteErr
}

func (f *fakeBackend) IsNotFound(err error) bool {
	return errors.Is(err, errFakeNotFound)
}

func TestDeriveKeyConsistent(t *testing.T) {
	fake := &fakeBackend{getErr: errFakeNotFound}
	restore := setBackendForTesting(fake)
	defer restore()

	key1, err := DeriveKey("test-info-1")
	require.NoError(t, err)
	require.NotNil(t, key1)

	key2, err := DeriveKey("test-info-1")
	require.NoError(t, err)
	require.NotNil(t, key2)

	assert.Equal(t, key1, key2, "same info string should produce same key")
	assert.Equal(t, 1, fake.getCalls, "master key should be loaded once")
	assert.Equal(t, 1, fake.setCalls, "missing master key should be created once")
}

func TestDeriveKeyDifferentInfoProducesDifferentKeys(t *testing.T) {
	restore := setBackendForTesting(&fakeBackend{
		stored: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	})
	defer restore()

	key1, err := DeriveKey("info-a")
	require.NoError(t, err)

	key2, err := DeriveKey("info-b")
	require.NoError(t, err)

	assert.NotEqual(t, key1, key2, "different info strings should produce different keys")
}

func TestDecodeMasterKeyValid(t *testing.T) {
	// 32 bytes = 64 hex chars
	validHex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	key, err := decodeMasterKey(validHex)
	require.NoError(t, err)
	assert.NotNil(t, key)
	assert.Equal(t, byte(0x01), key[0])
}

func TestDecodeMasterKeyInvalidHex(t *testing.T) {
	_, err := decodeMasterKey("not-hex")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "corrupt master key")
}

func TestDecodeMasterKeyWrongSize(t *testing.T) {
	// Only 16 bytes
	shortHex := "0123456789abcdef0123456789abcdef"
	_, err := decodeMasterKey(shortHex)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "wrong size")
}
