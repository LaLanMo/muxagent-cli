// Package crypto provides NaCl-based encryption primitives for muxagent.
// It wraps golang.org/x/crypto/nacl/box and secretbox with safe defaults.
package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/nacl/box"
	"golang.org/x/crypto/nacl/secretbox"
)

const (
	// KeySize is the size of a Curve25519 key or symmetric key in bytes.
	KeySize = 32
	// NonceSize is the size of a NaCl nonce in bytes.
	NonceSize = 24
	// BoxOverhead is the size of the Poly1305 authentication tag in bytes.
	BoxOverhead = box.Overhead // 16 bytes
	// XChaCha20-Poly1305 nonce size.
	XChaChaNonceSize = chacha20poly1305.NonceSizeX
)

var (
	ErrDecryptionFailed = errors.New("crypto: decryption failed")
	ErrInvalidKeySize   = errors.New("crypto: invalid key size")
	ErrMessageTooShort  = errors.New("crypto: message too short")
)

// GenerateKeyPair creates a new Curve25519 keypair for use with Box encryption.
func GenerateKeyPair() (publicKey, privateKey *[32]byte, err error) {
	return box.GenerateKey(rand.Reader)
}

// GenerateX25519KeyPair creates a new X25519 keypair.
func GenerateX25519KeyPair() (publicKey, privateKey *[32]byte, err error) {
	return GenerateKeyPair()
}

// BoxSeal encrypts a message for a recipient's public key using authenticated
// encryption. The sender's private key is used to prove authenticity.
// Returns: [nonce:24][ciphertext+tag]
func BoxSeal(message []byte, recipientPub, senderPriv *[32]byte) ([]byte, error) {
	var nonce [NonceSize]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, err
	}

	// box.Seal appends the ciphertext to the first argument
	sealed := make([]byte, NonceSize)
	copy(sealed, nonce[:])
	sealed = box.Seal(sealed, message, &nonce, recipientPub, senderPriv)

	return sealed, nil
}

// BoxOpen decrypts a message that was sealed with BoxSeal.
// Expects format: [nonce:24][ciphertext+tag]
func BoxOpen(sealed []byte, senderPub, recipientPriv *[32]byte) ([]byte, error) {
	if len(sealed) < NonceSize+BoxOverhead {
		return nil, ErrMessageTooShort
	}

	var nonce [NonceSize]byte
	copy(nonce[:], sealed[:NonceSize])

	plaintext, ok := box.Open(nil, sealed[NonceSize:], &nonce, senderPub, recipientPriv)
	if !ok {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

// BoxSealAnonymous encrypts a message for a recipient's public key without
// revealing the sender's identity. Uses an ephemeral keypair.
// Returns: [ephemeral_pub:32][nonce:24][ciphertext+tag]
func BoxSealAnonymous(message []byte, recipientPub *[32]byte) ([]byte, error) {
	// Generate ephemeral keypair
	ephemeralPub, ephemeralPriv, err := GenerateKeyPair()
	if err != nil {
		return nil, err
	}

	var nonce [NonceSize]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, err
	}

	// Format: [ephemeral_pub:32][nonce:24][ciphertext]
	sealed := make([]byte, KeySize+NonceSize)
	copy(sealed[:KeySize], ephemeralPub[:])
	copy(sealed[KeySize:], nonce[:])
	sealed = box.Seal(sealed, message, &nonce, recipientPub, ephemeralPriv)

	return sealed, nil
}

// BoxOpenAnonymous decrypts a message that was sealed with BoxSealAnonymous.
// Expects format: [ephemeral_pub:32][nonce:24][ciphertext+tag]
func BoxOpenAnonymous(sealed []byte, recipientPub, recipientPriv *[32]byte) ([]byte, error) {
	minLen := KeySize + NonceSize + BoxOverhead
	if len(sealed) < minLen {
		return nil, ErrMessageTooShort
	}

	var ephemeralPub [KeySize]byte
	copy(ephemeralPub[:], sealed[:KeySize])

	var nonce [NonceSize]byte
	copy(nonce[:], sealed[KeySize:KeySize+NonceSize])

	plaintext, ok := box.Open(nil, sealed[KeySize+NonceSize:], &nonce, &ephemeralPub, recipientPriv)
	if !ok {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

// SecretBoxSeal encrypts a message with a symmetric key using XSalsa20-Poly1305.
// Returns: [nonce:24][ciphertext+tag]
func SecretBoxSeal(message []byte, key *[32]byte) ([]byte, error) {
	var nonce [NonceSize]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, err
	}

	sealed := make([]byte, NonceSize)
	copy(sealed, nonce[:])
	sealed = secretbox.Seal(sealed, message, &nonce, key)

	return sealed, nil
}

// SecretBoxOpen decrypts a message that was sealed with SecretBoxSeal.
// Expects format: [nonce:24][ciphertext+tag]
func SecretBoxOpen(sealed []byte, key *[32]byte) ([]byte, error) {
	if len(sealed) < NonceSize+secretbox.Overhead {
		return nil, ErrMessageTooShort
	}

	var nonce [NonceSize]byte
	copy(nonce[:], sealed[:NonceSize])

	plaintext, ok := secretbox.Open(nil, sealed[NonceSize:], &nonce, key)
	if !ok {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

// XChaChaSeal encrypts a message with XChaCha20-Poly1305.
// Returns: [nonce:24][ciphertext+tag]
func XChaChaSeal(message []byte, key *[32]byte, aad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key[:])
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := aead.Seal(nil, nonce, message, aad)
	return append(nonce, ciphertext...), nil
}

// XChaChaOpen decrypts a message that was sealed with XChaChaSeal.
// Expects format: [nonce:24][ciphertext+tag]
func XChaChaOpen(sealed []byte, key *[32]byte, aad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key[:])
	if err != nil {
		return nil, err
	}

	if len(sealed) < aead.NonceSize()+aead.Overhead() {
		return nil, ErrMessageTooShort
	}

	nonce := sealed[:aead.NonceSize()]
	ciphertext := sealed[aead.NonceSize():]

	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrDecryptionFailed
	}
	return plaintext, nil
}

// DeriveKeyFromBytes derives a 32-byte key from arbitrary input using HKDF.
// The input should be high-entropy (e.g., a 32-byte master key from the OS keychain).
func DeriveKeyFromBytes(input []byte, info []byte) (*[32]byte, error) {
	reader := hkdf.New(sha256.New, input, nil, info)

	var key [32]byte
	if _, err := io.ReadFull(reader, key[:]); err != nil {
		return nil, err
	}

	return &key, nil
}

// RandomBytes generates n cryptographically secure random bytes.
func RandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return nil, err
	}
	return b, nil
}

// GenerateEd25519KeyPair generates a new Ed25519 keypair for signing.
func GenerateEd25519KeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

// Sign signs a message with an Ed25519 private key.
func Sign(message []byte, privateKey ed25519.PrivateKey) []byte {
	return ed25519.Sign(privateKey, message)
}

// SignBase64 signs a message and returns the signature as base64.
func SignBase64(message string, privateKey ed25519.PrivateKey) string {
	signature := Sign([]byte(message), privateKey)
	return base64.StdEncoding.EncodeToString(signature)
}

// BuildSignatureMessage constructs the message to sign for registration.
// Format: machineID + ":" + timestamp
func BuildSignatureMessage(machineID string, timestamp int64) string {
	return fmt.Sprintf("%s:%d", machineID, timestamp)
}

// BuildApprovalMessage constructs the master approval message.
// Format: muxagent-approve-v1|request_id|machine_sign_pub|machine_enc_pub|relay_challenge|expires_at
func BuildApprovalMessage(requestID, machineSignPubB64, machineEncPubB64, relayChallengeB64 string, expiresAt int64) string {
	return strings.Join([]string{
		"muxagent-approve-v1",
		requestID,
		machineSignPubB64,
		machineEncPubB64,
		relayChallengeB64,
		strconv.FormatInt(expiresAt, 10),
	}, "|")
}

// BuildMachineAuthMessage constructs the machine auth challenge message.
// Format: muxagent-machine-auth-v1|machine_id|nonce
func BuildMachineAuthMessage(machineID, nonceB64 string) string {
	return strings.Join([]string{
		"muxagent-machine-auth-v1",
		machineID,
		nonceB64,
	}, "|")
}

// BuildSessionInitMessage constructs the session-init signing message.
// Format: muxagent-session-init-v1|machine_id|client_ephemeral_pub
func BuildSessionInitMessage(machineID, clientEphemeralPubB64 string) string {
	return strings.Join([]string{
		"muxagent-session-init-v1",
		machineID,
		clientEphemeralPubB64,
	}, "|")
}

// BuildSessionAckMessage constructs the session-ack signing message.
// Format: muxagent-session-ack-v1|machine_id|machine_ephemeral_pub
func BuildSessionAckMessage(machineID, machineEphemeralPubB64 string) string {
	return strings.Join([]string{
		"muxagent-session-ack-v1",
		machineID,
		machineEphemeralPubB64,
	}, "|")
}

// KeyringUpdatePayload represents a signed keyring update payload.
type KeyringUpdatePayload struct {
	MasterID                       string
	Seq                            int
	PrevHash                       string
	Action                         string
	TargetMasterSignPub            string
	TargetMasterEncPub             string
	SignerMasterSignKeyFingerprint string
}

// BuildKeyringUpdateMessage constructs the keyring update message.
// Format: muxagent-keyring-update-v1|master_id|seq|prev_hash|action|target_master_sign_pub|target_master_enc_pub|signer_master_sign_key_fingerprint
func BuildKeyringUpdateMessage(payload KeyringUpdatePayload) string {
	return strings.Join([]string{
		"muxagent-keyring-update-v1",
		payload.MasterID,
		strconv.Itoa(payload.Seq),
		payload.PrevHash,
		payload.Action,
		payload.TargetMasterSignPub,
		payload.TargetMasterEncPub,
		payload.SignerMasterSignKeyFingerprint,
	}, "|")
}

// HashKeyFingerprint returns a stable fingerprint for a public key (SHA-256, base64url without padding).
func HashKeyFingerprint(pub []byte) string {
	sum := sha256.Sum256(pub)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// HashBytes returns a stable hash for arbitrary payload bytes (SHA-256, base64url without padding).
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Verify verifies an Ed25519 signature.
func Verify(publicKey ed25519.PublicKey, message, signature []byte) bool {
	return ed25519.Verify(publicKey, message, signature)
}
