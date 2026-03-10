// Package auth provides authentication and credential management for muxagent.
package auth

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/LaLanMo/muxagent-cli/internal/crypto"
	"github.com/LaLanMo/muxagent-cli/internal/localkey"
	"github.com/LaLanMo/muxagent-cli/internal/privdir"
)

const (
	credentialsVersion = 4
	localKeyInfo       = "muxagent-local-v1"
)

// Credentials stores the authenticated machine's keys and keyring state.
type Credentials struct {
	Version   int    `json:"version"`
	MachineID string `json:"machine_id"`
	MasterID  string `json:"master_id"`

	Keyring KeyringState   `json:"keyring"`
	Keys    CredentialKeys `json:"keys"`
}

// KeyringState represents the master keyring snapshot.
type KeyringState struct {
	MasterID string          `json:"master_id"`
	Seq      int             `json:"seq"`
	HeadHash string          `json:"head_hash"`
	Keys     []MasterKeyInfo `json:"keys"`
}

// MasterKeyInfo describes a master device key.
type MasterKeyInfo struct {
	MasterSignKeyFingerprint string `json:"master_sign_key_fingerprint"`
	MasterSignPub            string `json:"master_sign_pub"`
	MasterEncPub             string `json:"master_enc_pub"`
	RevokedAt                *int64 `json:"revoked_at,omitempty"`
}

// CredentialKeys contains the cryptographic keys.
type CredentialKeys struct {
	MachineSignPublicKey           string `json:"machine_sign_public_key"`
	MachineSignPrivateKeyEncrypted string `json:"machine_sign_private_key_encrypted"`
	MachineEncPublicKey            string `json:"machine_enc_public_key"`
	MachineEncPrivateKeyEncrypted  string `json:"machine_enc_private_key_encrypted"`
}

var (
	ErrNoCredentials      = errors.New("auth: no credentials found")
	ErrInvalidCredentials = errors.New("auth: invalid or corrupted credentials")
	ErrDecryptionFailed   = errors.New("auth: failed to decrypt private key")
	ErrStaleCredentials   = errors.New("auth: credentials format changed (pre-release), please log in again with `muxagent auth login`")
)

// CredentialsPath returns the path to the credentials file.
func CredentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".muxagent", "credentials.json"), nil
}

// SaveCredentials saves credentials to disk with the machine private keys encrypted.
// machineSignSeed should be the 32-byte Ed25519 seed, machineEncPriv should be 32 bytes.
func SaveCredentials(creds *Credentials, machineSignSeed, machineEncPriv []byte) error {
	path, err := CredentialsPath()
	if err != nil {
		return err
	}

	if err := privdir.Ensure(filepath.Dir(path)); err != nil {
		return err
	}

	localKey, err := localkey.DeriveKey(localKeyInfo)
	if err != nil {
		return err
	}

	signSealed, err := crypto.SecretBoxSeal(machineSignSeed, localKey)
	if err != nil {
		return err
	}
	encSealed, err := crypto.SecretBoxSeal(machineEncPriv, localKey)
	if err != nil {
		return err
	}

	creds.Keys.MachineSignPrivateKeyEncrypted = base64.StdEncoding.EncodeToString(signSealed)
	creds.Keys.MachineEncPrivateKeyEncrypted = base64.StdEncoding.EncodeToString(encSealed)
	creds.Version = credentialsVersion

	payload, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, payload, 0o600)
}

// LoadCredentials loads credentials from disk and decrypts the machine private keys.
// Returns the credentials, the Ed25519 private key, and the X25519 private key.
func LoadCredentials() (*Credentials, ed25519.PrivateKey, *[32]byte, error) {
	path, err := CredentialsPath()
	if err != nil {
		return nil, nil, nil, err
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil, nil, ErrNoCredentials
		}
		return nil, nil, nil, err
	}

	var creds Credentials
	if err := json.Unmarshal(payload, &creds); err != nil {
		return nil, nil, nil, ErrInvalidCredentials
	}

	if creds.Version < credentialsVersion {
		return nil, nil, nil, ErrStaleCredentials
	}

	localKey, err := localkey.DeriveKey(localKeyInfo)
	if err != nil {
		return nil, nil, nil, err
	}

	signSealed, err := base64.StdEncoding.DecodeString(creds.Keys.MachineSignPrivateKeyEncrypted)
	if err != nil {
		return nil, nil, nil, ErrInvalidCredentials
	}
	signSeed, err := crypto.SecretBoxOpen(signSealed, localKey)
	if err != nil {
		return nil, nil, nil, ErrDecryptionFailed
	}
	if len(signSeed) != ed25519.SeedSize {
		return nil, nil, nil, ErrInvalidCredentials
	}
	machineSignPriv := ed25519.NewKeyFromSeed(signSeed)

	encSealed, err := base64.StdEncoding.DecodeString(creds.Keys.MachineEncPrivateKeyEncrypted)
	if err != nil {
		return nil, nil, nil, ErrInvalidCredentials
	}
	encPrivBytes, err := crypto.SecretBoxOpen(encSealed, localKey)
	if err != nil {
		return nil, nil, nil, ErrDecryptionFailed
	}
	if len(encPrivBytes) != 32 {
		return nil, nil, nil, ErrInvalidCredentials
	}
	var machineEncPriv [32]byte
	copy(machineEncPriv[:], encPrivBytes)

	return &creds, machineSignPriv, &machineEncPriv, nil
}

// HasCredentials returns true if credentials file exists.
func HasCredentials() bool {
	path, err := CredentialsPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// ClearCredentials removes the credentials file.
func ClearCredentials() error {
	path, err := CredentialsPath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// GetMachineSignPublicKey decodes and returns the machine signing public key.
func (c *Credentials) GetMachineSignPublicKey() (ed25519.PublicKey, error) {
	decoded, err := base64.StdEncoding.DecodeString(c.Keys.MachineSignPublicKey)
	if err != nil {
		return nil, err
	}
	if len(decoded) != ed25519.PublicKeySize {
		return nil, ErrInvalidCredentials
	}
	return ed25519.PublicKey(decoded), nil
}

// GetMachineEncPublicKey decodes and returns the machine encryption public key.
func (c *Credentials) GetMachineEncPublicKey() (*[32]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(c.Keys.MachineEncPublicKey)
	if err != nil {
		return nil, err
	}
	if len(decoded) != 32 {
		return nil, ErrInvalidCredentials
	}
	var key [32]byte
	copy(key[:], decoded)
	return &key, nil
}

// FindMasterKey locates a master key by fingerprint.
func (c *Credentials) FindMasterKey(masterSignKeyFingerprint string) *MasterKeyInfo {
	for i := range c.Keyring.Keys {
		if c.Keyring.Keys[i].MasterSignKeyFingerprint == masterSignKeyFingerprint {
			return &c.Keyring.Keys[i]
		}
	}
	return nil
}

// NewCredentials creates a new Credentials struct with provided keys and keyring.
func NewCredentials(machineID, masterID string, keyring KeyringState, machineSignPub ed25519.PublicKey, machineEncPub *[32]byte) (*Credentials, error) {
	if machineEncPub == nil {
		return nil, ErrInvalidCredentials
	}

	creds := &Credentials{
		Version:   credentialsVersion,
		MachineID: machineID,
		MasterID:  masterID,
		Keyring:   keyring,
		Keys: CredentialKeys{
			MachineSignPublicKey: base64.StdEncoding.EncodeToString(machineSignPub),
			MachineEncPublicKey:  base64.StdEncoding.EncodeToString(machineEncPub[:]),
		},
	}

	return creds, nil
}
