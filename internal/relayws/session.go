package relayws

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const (
	sessionInfo = "muxagent-session-v1"
	aadPrefix   = "muxagent-aad-v1"
)

type Session struct {
	machineID string
	connEpoch uint64
	key       [32]byte
}

func newSession(machineID string, key [32]byte, connEpoch uint64) *Session {
	return &Session{machineID: machineID, connEpoch: connEpoch, key: key}
}

func deriveSessionKey(sharedSecret []byte, transcript string) ([32]byte, error) {
	salt := sha256.Sum256([]byte(transcript))
	reader := hkdf.New(sha256.New, sharedSecret, salt[:], []byte(sessionInfo))
	var key [32]byte
	if _, err := io.ReadFull(reader, key[:]); err != nil {
		return key, err
	}
	return key, nil
}

func (s *Session) encrypt(msgType, msgID string, plaintext []byte) (nonceB64 string, ciphertextB64 string, err error) {
	aead, err := chacha20poly1305.NewX(s.key[:])
	if err != nil {
		return "", "", err
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return "", "", err
	}
	aad := []byte(buildAAD(s.machineID, msgType, msgID))
	ciphertext := aead.Seal(nil, nonce, plaintext, aad)
	return base64.StdEncoding.EncodeToString(nonce), base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (s *Session) decrypt(msgType, msgID, nonceB64, ciphertextB64 string) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(s.key[:])
	if err != nil {
		return nil, err
	}
	nonce, err := base64.StdEncoding.DecodeString(nonceB64)
	if err != nil {
		return nil, err
	}
	if len(nonce) != chacha20poly1305.NonceSizeX {
		return nil, fmt.Errorf("invalid nonce size")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return nil, err
	}
	aad := []byte(buildAAD(s.machineID, msgType, msgID))
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}

func buildAAD(machineID, msgType, msgID string) string {
	return aadPrefix + "|" + machineID + "|" + msgType + "|" + msgID
}
