package keyring

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LaLanMo/muxagent-cli/internal/auth"
	"github.com/LaLanMo/muxagent-cli/internal/crypto"
)

var (
	ErrInvalidToken  = errors.New("keyring: invalid token")
	ErrTokenExpired  = errors.New("keyring: token expired")
	ErrUnauthorized  = errors.New("keyring: unauthorized signer")
	ErrInvalidUpdate = errors.New("keyring: invalid update")
)

type Manager struct {
	mu    sync.RWMutex
	state auth.KeyringState
	keys  map[string]*auth.MasterKeyInfo
}

func NewManager(state auth.KeyringState) *Manager {
	keys := make(map[string]*auth.MasterKeyInfo, len(state.Keys))
	for i := range state.Keys {
		key := state.Keys[i]
		keys[key.MasterSignKeyFingerprint] = &key
	}
	return &Manager{state: state, keys: keys}
}

func (m *Manager) MasterID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state.MasterID
}

func (m *Manager) Seq() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state.Seq
}

func (m *Manager) HeadHash() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state.HeadHash
}

func (m *Manager) State() auth.KeyringState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := auth.KeyringState{
		MasterID: m.state.MasterID,
		Seq:      m.state.Seq,
		HeadHash: m.state.HeadHash,
		Keys:     make([]auth.MasterKeyInfo, 0, len(m.keys)),
	}
	for _, key := range m.keys {
		out.Keys = append(out.Keys, *key)
	}
	return out
}

func (m *Manager) IsAuthorized(fingerprint string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key, ok := m.keys[fingerprint]
	return ok && key.RevokedAt == nil
}

func (m *Manager) SignPub(fingerprint string) (ed25519.PublicKey, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key, ok := m.keys[fingerprint]
	if !ok {
		return nil, false
	}
	if key.RevokedAt != nil {
		return nil, false
	}
	pub, err := base64.StdEncoding.DecodeString(key.MasterSignPub)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return nil, false
	}
	return ed25519.PublicKey(pub), true
}

type MachineTokenClaims struct {
	MasterID    string
	MachineID   string
	Fingerprint string
	ExpiresAt   time.Time
}

func (m *Manager) VerifyMachineToken(token string) (MachineTokenClaims, error) {
	payload, sig, err := decodeToken(token)
	if err != nil {
		return MachineTokenClaims{}, err
	}
	parts := strings.Split(payload, "|")
	if len(parts) != 5 || parts[0] != "muxagent-machine-token-v1" {
		return MachineTokenClaims{}, ErrInvalidToken
	}
	masterID := parts[1]
	machineID := parts[2]
	fingerprint := parts[3]
	expiresAt, err := parseUnix(parts[4])
	if err != nil {
		return MachineTokenClaims{}, ErrInvalidToken
	}
	if time.Now().After(expiresAt) {
		return MachineTokenClaims{}, ErrTokenExpired
	}
	pub, ok := m.SignPub(fingerprint)
	if !ok {
		return MachineTokenClaims{}, ErrUnauthorized
	}
	if !crypto.Verify(pub, []byte(payload), sig) {
		return MachineTokenClaims{}, ErrInvalidToken
	}
	return MachineTokenClaims{
		MasterID:    masterID,
		MachineID:   machineID,
		Fingerprint: fingerprint,
		ExpiresAt:   expiresAt,
	}, nil
}

type updatesEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type keyringUpdatesResponse struct {
	MasterID string                `json:"master_id"`
	FromSeq  int                   `json:"from_seq"`
	ToSeq    int                   `json:"to_seq"`
	HeadHash string                `json:"head_hash"`
	Updates  []keyringUpdateRecord `json:"updates"`
}

type keyringUpdateRecord struct {
	Seq                            int    `json:"seq"`
	PrevHash                       string `json:"prev_hash"`
	UpdateHash                     string `json:"update_hash"`
	Action                         string `json:"action"`
	TargetMasterSignKeyFingerprint string `json:"target_master_sign_key_fingerprint"`
	TargetMasterSignPub            string `json:"target_master_sign_pub"`
	TargetMasterEncPub             string `json:"target_master_enc_pub"`
	SignerMasterSignKeyFingerprint string `json:"signer_master_sign_key_fingerprint"`
	Signature                      string `json:"signature"`
	CreatedAt                      int64  `json:"created_at"`
}

func (m *Manager) Sync(ctx context.Context, relayHTTPURL, accessToken string) error {
	m.mu.RLock()
	masterID := m.state.MasterID
	fromSeq := m.state.Seq
	m.mu.RUnlock()

	url := fmt.Sprintf("%s/v1/keyring/%s/updates?from_seq=%d", strings.TrimRight(relayHTTPURL, "/"), masterID, fromSeq)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("keyring updates http status: %s", resp.Status)
	}

	var env updatesEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return err
	}
	if env.Code != 0 {
		return fmt.Errorf("keyring updates error (code %d): %s", env.Code, env.Message)
	}

	var payload keyringUpdatesResponse
	if err := json.Unmarshal(env.Data, &payload); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, update := range payload.Updates {
		if update.Seq != m.state.Seq+1 {
			return ErrInvalidUpdate
		}
		if update.PrevHash != m.state.HeadHash {
			return ErrInvalidUpdate
		}
		signer, ok := m.keys[update.SignerMasterSignKeyFingerprint]
		if !ok || signer.RevokedAt != nil {
			return ErrUnauthorized
		}
		signerPub, err := base64.StdEncoding.DecodeString(signer.MasterSignPub)
		if err != nil || len(signerPub) != ed25519.PublicKeySize {
			return ErrInvalidUpdate
		}
		msg := crypto.BuildKeyringUpdateMessage(crypto.KeyringUpdatePayload{
			MasterID:                       payload.MasterID,
			Seq:                            update.Seq,
			PrevHash:                       update.PrevHash,
			Action:                         update.Action,
			TargetMasterSignPub:            update.TargetMasterSignPub,
			TargetMasterEncPub:             update.TargetMasterEncPub,
			SignerMasterSignKeyFingerprint: update.SignerMasterSignKeyFingerprint,
		})
		if update.UpdateHash != crypto.HashBytes([]byte(msg)) {
			return ErrInvalidUpdate
		}
		sig, err := base64.StdEncoding.DecodeString(update.Signature)
		if err != nil {
			return ErrInvalidUpdate
		}
		if !crypto.Verify(ed25519.PublicKey(signerPub), []byte(msg), sig) {
			return ErrInvalidUpdate
		}

		switch update.Action {
		case "add":
			m.keys[update.TargetMasterSignKeyFingerprint] = &auth.MasterKeyInfo{
				MasterSignKeyFingerprint: update.TargetMasterSignKeyFingerprint,
				MasterSignPub:            update.TargetMasterSignPub,
				MasterEncPub:             update.TargetMasterEncPub,
			}
		case "revoke":
			if key, ok := m.keys[update.TargetMasterSignKeyFingerprint]; ok {
				revokedAt := update.CreatedAt
				key.RevokedAt = &revokedAt
			}
		default:
			return ErrInvalidUpdate
		}

		m.state.Seq = update.Seq
		m.state.HeadHash = update.UpdateHash
	}

	return nil
}

func decodeToken(token string) (string, []byte, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return "", nil, ErrInvalidToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", nil, ErrInvalidToken
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", nil, ErrInvalidToken
	}
	return string(payload), sig, nil
}

func parseUnix(raw string) (time.Time, error) {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(value, 0).UTC(), nil
}
